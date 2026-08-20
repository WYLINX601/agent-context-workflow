package supervisor

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/linx-workbench/multica-hermes-gateway/internal/config"
	_ "modernc.org/sqlite"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	cfg := config.Defaults()
	cfg.StateDB = filepath.Join(t.TempDir(), "state.db")
	cfg.Supervisor.RuntimeDB = filepath.Join(t.TempDir(), "runtime.db")
	cfg.Supervisor.LeaseTTL = "1h"
	cfg.Supervisor.IsolatedIdleTimeout = "1ms"
	manager, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	runtime := &managedRuntime{
		record: registryRuntime{RuntimeID: "runtime-test", GatewayIdentity: cfg.GatewayIdentity(), GatewayURL: cfg.Gateway.URL, StatusURL: cfg.Gateway.StatusURL, Ownership: "managed-isolated", State: "READY", Generation: 7},
		leases: make(map[string]*lease), pins: make(map[string]struct{}), lastActivity: time.Now(),
	}
	manager.runtimes[cfg.GatewayIdentity()+"\x00"] = runtime
	return manager
}

func TestIPCAcquireIdempotentAndGenerationFenced(t *testing.T) {
	manager := testManager(t)
	endpoint := filepath.Join(os.TempDir(), "mhg-test-"+randomID("sock"))
	server, err := NewServer(manager, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(server.Close)
	client := NewClient(endpoint, "adapter-test")
	params := AcquireParams{GatewayIdentity: manager.cfg.GatewayIdentity(), Scope: "isolated"}
	first, err := client.AcquireRuntimeWithRequestID(context.Background(), "request-1", params)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	requestLease := manager.requests["adapter-test\x00request-1"]
	manager.mu.Unlock()
	if requestLease != first.LeaseID {
		t.Fatalf("server did not record request idempotency key: %q vs %q", requestLease, first.LeaseID)
	}
	second, err := client.AcquireRuntimeWithRequestID(context.Background(), "request-1", params)
	if err != nil {
		t.Fatal(err)
	}
	if first.LeaseID != second.LeaseID || first.Generation != 7 {
		t.Fatalf("acquire was not idempotent: %+v %+v", first, second)
	}
	if err := client.HeartbeatLease(context.Background(), first.LeaseID, first.Generation+1); err == nil {
		t.Fatal("expected generation fencing error")
	} else {
		var coded *SupervisorError
		if !errors.As(err, &coded) || coded.Code != CodeGenerationChanged {
			t.Fatalf("unexpected fencing error: %v", err)
		}
	}
	if err := client.PinTurn(context.Background(), first.LeaseID, "session-1", first.Generation); err != nil {
		t.Fatal(err)
	}
	status, err := client.GetRuntimeStatus(context.Background(), first.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if status.PinCount != 1 || status.LeaseCount != 1 {
		t.Fatalf("unexpected runtime status: %+v", status)
	}
	if err := client.UnpinTurn(context.Background(), first.LeaseID, "session-1", first.Generation); err != nil {
		t.Fatal(err)
	}
	if err := client.ReleaseRuntime(context.Background(), first.LeaseID, first.Generation); err != nil {
		t.Fatal(err)
	}
}

func TestPinBlocksIsolatedAutomaticReapAndSharedBoundaries(t *testing.T) {
	manager := testManager(t)
	runtime := manager.runtimes[manager.cfg.GatewayIdentity()+"\x00"]
	runtime.lastActivity = time.Now().Add(-time.Hour)
	runtime.leases["lease-1"] = &lease{id: "lease-1", runtimeID: runtime.record.RuntimeID, clientID: "client", generation: runtime.record.Generation, expiresAt: time.Now().Add(time.Hour)}
	manager.leases["lease-1"] = runtime.leases["lease-1"]
	runtime.pins["lease-1\x00turn"] = struct{}{}
	manager.reap()
	if _, ok := manager.runtimes[manager.cfg.GatewayIdentity()+"\x00"]; !ok {
		t.Fatal("pinned isolated runtime was reaped")
	}
	delete(runtime.pins, "lease-1\x00turn")
	delete(runtime.leases, "lease-1")
	delete(manager.leases, "lease-1")
	manager.reap()
	if _, ok := manager.runtimes[manager.cfg.GatewayIdentity()+"\x00"]; ok {
		t.Fatal("unpinned idle isolated runtime was not reaped")
	}
	for _, ownership := range []string{"adopted-shared", "managed-shared"} {
		candidate := &managedRuntime{record: registryRuntime{Ownership: ownership}, lastActivity: time.Now().Add(-time.Hour), leases: map[string]*lease{}, pins: map[string]struct{}{}}
		if shouldAutoReap(candidate, time.Now(), time.Second) {
			t.Fatalf("%s should not auto reap", ownership)
		}
	}
}

func TestIdentityMatchingRequiresAllProofFields(t *testing.T) {
	expected := processIdentity{PID: 42, ProcessStartToken: "start-1", CommandFingerprint: "cmd-1", Endpoint: "ws://127.0.0.1:9119/api/ws"}
	if !identityMatches(expected, expected) {
		t.Fatal("identical identity should match")
	}
	changed := expected
	changed.PID = 43
	if identityMatches(expected, changed) {
		t.Fatal("PID reuse must not match")
	}
	changed = expected
	changed.ProcessStartToken = "unknown"
	if identityMatches(expected, changed) {
		t.Fatal("unknown process start token must not match")
	}
}

func TestRegistryRoundTripPreservesTurnPins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	db, err := openRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	runtimes := []registryRuntime{{RuntimeID: "runtime-1", GatewayIdentity: "ws://127.0.0.1:9119/api/ws", Scope: "isolated", GatewayURL: "ws://127.0.0.1:9119/api/ws", StatusURL: "http://127.0.0.1:9119/api/status", Ownership: "managed-isolated", State: "READY", Generation: 3, EnvironmentFingerprint: "env-1"}}
	leases := []registryLease{{LeaseID: "lease-1", RuntimeID: "runtime-1", ClientID: "client-1", RequestID: "request-1", Generation: 3, ExpiresAt: time.Now().Add(time.Hour)}}
	pins := []registryPin{{ClientID: "client-1", LeaseID: "lease-1", RuntimeID: "runtime-1", SessionID: "session-1", Generation: 3, TurnDeadline: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), State: "ACTIVE"}}
	operations := []registryOperation{{OperationID: "operation-1", ClientID: "client-1", RequestID: "request-1", RuntimeID: "runtime-1", GatewayIdentity: runtimes[0].GatewayIdentity, Scope: "isolated", GatewayURL: runtimes[0].GatewayURL, StatusURL: runtimes[0].StatusURL, Endpoint: runtimes[0].GatewayIdentity, LaunchNonce: "launch-1", EnvironmentFingerprint: "env-1", CommandFingerprint: "fingerprint-1", State: "STARTING", CreatedAt: nowString(), UpdatedAt: nowString()}}
	if err := db.Save(runtimes, leases, pins, operations); err != nil {
		t.Fatal(err)
	}
	gotRuntimes, gotLeases, gotPins, gotOperations, err := db.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(gotRuntimes) != 1 || len(gotLeases) != 1 || len(gotPins) != 1 || gotPins[0].SessionID != "session-1" {
		t.Fatalf("registry round trip lost lifecycle state: %v %v %v", gotRuntimes, gotLeases, gotPins)
	}
	if gotPins[0].ClientID != "client-1" || len(gotOperations) != 1 || gotOperations[0].OperationID != "operation-1" {
		t.Fatalf("registry round trip lost ownership/journal state: pins=%v operations=%v", gotPins, gotOperations)
	}
	if gotRuntimes[0].EnvironmentFingerprint != "env-1" || gotOperations[0].EnvironmentFingerprint != "env-1" {
		t.Fatalf("registry round trip lost environment fingerprint: runtime=%v operation=%v", gotRuntimes[0], gotOperations[0])
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRegistryIsSingleWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	first, err := openRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := openRegistry(path); err == nil {
		_ = second.Close()
		t.Fatal("second Supervisor unexpectedly acquired runtime registry")
	}
}

func TestExpiredLeaseKeepsTurnPinUntilOwnerUnpins(t *testing.T) {
	manager := testManager(t)
	runtime := manager.runtimes[manager.cfg.GatewayIdentity()+"\x00"]
	lease := &lease{id: "lease-expiring", runtimeID: runtime.record.RuntimeID, clientID: "client-expiring", requestID: "request-expiring", generation: runtime.record.Generation, expiresAt: time.Now().Add(time.Hour)}
	runtime.leases[lease.id] = lease
	manager.leases[lease.id] = lease
	manager.requests[lease.clientID+"\x00"+lease.requestID] = lease.id
	if err := manager.Pin(context.Background(), lease.clientID, lease.id, "turn-expiring", lease.generation); err != nil {
		t.Fatal(err)
	}
	lease.expiresAt = time.Now().Add(-time.Second)
	manager.reap()
	if _, ok := manager.leases[lease.id]; ok {
		t.Fatal("expired lease was not removed")
	}
	pinKey := lease.id + "\x00turn-expiring"
	if _, ok := runtime.pins[pinKey]; !ok {
		t.Fatal("active turn pin was removed together with expired lease")
	}
	if err := manager.Unpin(context.Background(), "other-client", lease.id, "turn-expiring", lease.generation); err == nil {
		t.Fatal("a different client was allowed to clear an expired lease's turn pin")
	}
	if err := manager.Unpin(context.Background(), lease.clientID, lease.id, "turn-expiring", lease.generation); err != nil {
		t.Fatalf("owner could not unpin after lease expiry: %v", err)
	}
	if _, ok := runtime.pins[pinKey]; ok {
		t.Fatal("turn pin remained after owner unpinned")
	}
}

func TestReleaseRemovesPersistedLease(t *testing.T) {
	manager := testManager(t)
	key := manager.cfg.GatewayIdentity() + "\x00"
	runtime := manager.runtimes[key]
	item := &lease{id: "lease-release", runtimeID: runtime.record.RuntimeID, clientID: "client-release", requestID: "request-release", generation: runtime.record.Generation, expiresAt: time.Now().Add(time.Hour)}
	runtime.leases[item.id] = item
	manager.leases[item.id] = item
	manager.requests[item.clientID+"\x00"+item.requestID] = item.id
	manager.mu.Lock()
	if err := manager.persistLocked(); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()

	if err := manager.Release(context.Background(), item.clientID, item.id, item.generation); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	_, leases, _, _, err := manager.registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 0 {
		t.Fatalf("released lease remained in runtime registry: %+v", leases)
	}
}

func TestStopDeletesStaleRuntimeEntry(t *testing.T) {
	manager := testManager(t)
	oldKey := manager.cfg.GatewayIdentity() + "\x00"
	runtime := manager.runtimes[oldKey]
	delete(manager.runtimes, oldKey)
	staleKey := oldKey + "stale\x00" + runtime.record.RuntimeID
	manager.runtimes[staleKey] = runtime

	if err := manager.Stop(context.Background(), "client-stop", runtime.record.RuntimeID, runtime.record.Generation); err != nil {
		t.Fatalf("stop failed for stale runtime: %v", err)
	}
	if _, ok := manager.runtimes[staleKey]; ok {
		t.Fatal("stale runtime entry remained after explicit stop")
	}
}

func TestSharedProfileValidationFailsClosedWithoutMultiplexServing(t *testing.T) {
	payload := `{"profiles":[{"name":"kahn"}],"gateway_mode":"single","gateways":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	cfg := config.Defaults()
	cfg.Gateway.StatusURL = server.URL + "/api/status"
	cfg.Gateway.ConnectTimeout = "1s"
	err := validateSharedProfile(context.Background(), cfg, "kahn")
	var coded *SupervisorError
	if !errors.As(err, &coded) || coded.Code != CodeSharedMultiplex {
		t.Fatalf("expected shared multiplex fail-closed error, got %v", err)
	}

	payload = `{"profiles":[{"name":"kahn"}],"gateway_mode":"multiplex","gateways":[{"served_profiles":["default"]}]}`
	err = validateSharedProfile(context.Background(), cfg, "kahn")
	if !errors.As(err, &coded) || coded.Code != CodeProfileNotServed {
		t.Fatalf("expected profile-not-served error, got %v", err)
	}

	payload = `{"profiles":[{"name":"kahn"}],"gateway_mode":"multiplex","gateways":[{"served_profiles":["kahn"]}]}`
	if err := validateSharedProfile(context.Background(), cfg, "kahn"); err != nil {
		t.Fatalf("served multiplex profile was rejected: %v", err)
	}
}

func TestManagedRuntimeTokenUsesPrivateOpaqueReference(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission assertion")
	}
	cfg := config.Defaults()
	cfg.StateDB = filepath.Join(t.TempDir(), "state.db")
	cfg.Supervisor.RuntimeDB = filepath.Join(t.TempDir(), "runtime.db")
	manager, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	ref, token, err := manager.prepareRuntimeToken("runtime-token-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.removeRuntimeToken(ref)
	info, err := os.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("runtime token file permissions are %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != token || len(token) != 64 {
		t.Fatal("runtime token file did not contain the generated opaque token")
	}
	runtime := &managedRuntime{record: registryRuntime{RuntimeID: "runtime-token-test", TokenRef: ref}}
	if got := runtimeInfo(runtime, "").TokenRef; got != ref || strings.Contains(got, token) {
		t.Fatal("runtime info exposed token contents instead of an opaque reference")
	}
}

func TestHealthRecoveryWaitsForPinnedTurn(t *testing.T) {
	manager := testManager(t)
	key := manager.cfg.GatewayIdentity() + "\x00"
	runtime := manager.runtimes[key]
	runtime.record.State = "READY"
	runtime.record.PID = 999999
	runtime.pins["lease-1\x00turn-1"] = struct{}{}
	if manager.markUnhealthy(key, time.Now()) {
		t.Fatal("health monitor must not restart a pinned runtime")
	}
	if runtime.record.State != "DEGRADED" || runtime.restarting {
		t.Fatalf("unexpected pinned degraded state: %+v", runtime.record)
	}
	delete(runtime.pins, "lease-1\x00turn-1")
	if !manager.markUnhealthy(key, time.Now()) {
		t.Fatal("unpinned degraded runtime should schedule bounded recovery")
	}
	if runtime.record.State != "RESTARTING" {
		t.Fatalf("expected restarting state, got %s", runtime.record.State)
	}
}

func TestRuntimeDBUsesSQLiteSchema(t *testing.T) {
	cfg := config.Defaults()
	cfg.StateDB = filepath.Join(t.TempDir(), "state.db")
	cfg.Supervisor.RuntimeDB = filepath.Join(t.TempDir(), "runtime.db")
	manager, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.Close()
	db, err := sql.Open("sqlite", cfg.Supervisor.RuntimeDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tableCount int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('runtimes','leases')`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 2 {
		t.Fatalf("runtime.db does not contain SQLite registry tables: %d", tableCount)
	}
}

func TestSupervisorRestartRestoresProvenRuntimeAndLease(t *testing.T) {
	cfg := config.Defaults()
	cfg.StateDB = filepath.Join(t.TempDir(), "state.db")
	cfg.Supervisor.RuntimeDB = filepath.Join(t.TempDir(), "runtime.db")
	manager, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := processIdentity{PID: os.Getpid(), ProcessStartToken: processStartToken(os.Getpid()), CommandFingerprint: commandFingerprintForProcess(os.Getpid(), "", nil)}
	if identity.ProcessStartToken == "unknown" || identity.CommandFingerprint == "" {
		t.Skip("platform cannot provide process identity proof")
	}
	runtime := &managedRuntime{record: registryRuntime{RuntimeID: "runtime-restart", GatewayIdentity: cfg.GatewayIdentity(), Scope: "shared", GatewayURL: cfg.Gateway.URL, StatusURL: cfg.Gateway.StatusURL, Ownership: "managed-shared", State: "READY", PID: identity.PID, ProcessStartToken: identity.ProcessStartToken, CommandFingerprint: identity.CommandFingerprint, Generation: 4}, leases: map[string]*lease{}, pins: map[string]struct{}{}, lastActivity: time.Now()}
	lease := &lease{id: "lease-restart", runtimeID: runtime.record.RuntimeID, clientID: "adapter", requestID: "request", generation: 4, expiresAt: time.Now().Add(time.Hour)}
	runtime.leases[lease.id] = lease
	manager.runtimes[cfg.GatewayIdentity()] = runtime
	manager.leases[lease.id] = lease
	manager.requests["adapter\x00request"] = lease.id
	manager.mu.Lock()
	if err := manager.persistLocked(); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()
	manager.Close()

	restored, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.runtimeByIDLocked("runtime-restart") == nil {
		t.Fatal("runtime was not restored")
	}
	if restored.leases["lease-restart"] == nil {
		t.Fatal("lease was not restored")
	}
}

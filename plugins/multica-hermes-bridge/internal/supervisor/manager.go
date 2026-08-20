package supervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/linx-workbench/multica-hermes-gateway/internal/config"
	"github.com/linx-workbench/multica-hermes-gateway/internal/hermes"
)

type Manager struct {
	cfg        config.Config
	resolver   ProfileResolver
	registry   *registryDB
	instanceID string
	log        func(string, ...any)

	mu         sync.Mutex
	acquireMu  sync.Mutex
	runtimes   map[string]*managedRuntime
	leases     map[string]*lease
	requests   map[string]string
	operations map[string]*registryOperation
	stopReaper chan struct{}
	stopHealth chan struct{}
	closeOnce  sync.Once
	reaperWG   sync.WaitGroup
	healthWG   sync.WaitGroup
}

type managedRuntime struct {
	record         registryRuntime
	process        *managedProcess
	leases         map[string]*lease
	pins           map[string]struct{}
	pinOwners      map[string]string
	pinGenerations map[string]uint64
	pinDeadlines   map[string]time.Time
	ambiguousPins  map[string]bool
	lastActivity   time.Time
	healthFails    int
	nextRestart    time.Time
	restarting     bool
	reaping        bool
}

type lease struct {
	id         string
	runtimeID  string
	clientID   string
	requestID  string
	generation uint64
	expiresAt  time.Time
}

func NewManager(cfg config.Config, logf func(string, ...any)) (*Manager, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	registry, err := openRegistry(cfg.SupervisorRuntimeDB())
	if err != nil {
		return nil, err
	}
	manager := &Manager{
		cfg:        cfg,
		resolver:   ProfileResolver{Executable: cfg.Supervisor.HermesExecutable},
		registry:   registry,
		instanceID: randomID("sup"),
		log:        logf,
		runtimes:   make(map[string]*managedRuntime),
		leases:     make(map[string]*lease),
		requests:   make(map[string]string),
		operations: make(map[string]*registryOperation),
		stopReaper: make(chan struct{}),
		stopHealth: make(chan struct{}),
	}
	if err := manager.restore(); err != nil {
		_ = registry.Close()
		return nil, err
	}
	manager.reaperWG.Add(1)
	go manager.reapLoop()
	manager.healthWG.Add(1)
	go manager.healthLoop()
	return manager, nil
}

func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		close(m.stopReaper)
		close(m.stopHealth)
		m.reaperWG.Wait()
		m.healthWG.Wait()
		_ = m.registry.Close()
		// Managed processes deliberately survive a Supervisor restart. An external
		// service manager or the next Supervisor instance can recover them by
		// endpoint; no unproven PID is ever killed here.
	})
}

func (m *Manager) Acquire(ctx context.Context, clientID, requestID string, params AcquireParams) (RuntimeInfo, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(requestID) == "" {
		return RuntimeInfo{}, &SupervisorError{Code: CodeRuntimeUnavailable, Message: "client_id and request_id are required"}
	}
	scope := strings.TrimSpace(params.Scope)
	if scope == "" {
		scope = strings.TrimSpace(m.cfg.Supervisor.Scope)
	}
	if scope != "shared" && scope != "isolated" {
		return RuntimeInfo{}, &SupervisorError{Code: CodeRuntimeUnavailable, Message: "scope must be shared or isolated"}
	}
	requestKey := clientID + "\x00" + requestID
	m.acquireMu.Lock()
	defer m.acquireMu.Unlock()
	m.mu.Lock()
	if operation := m.operations[requestKey]; operation != nil {
		switch operation.State {
		case "FAILED":
			code := operation.ErrorCode
			if code == "" {
				code = CodeStartFailed
			}
			m.mu.Unlock()
			return RuntimeInfo{}, &SupervisorError{Code: code, Message: operation.ErrorMessage}
		case "STARTING":
			if runtime := m.runtimeByIDLocked(operation.RuntimeID); runtime == nil || runtime.record.State != "READY" {
				m.mu.Unlock()
				return RuntimeInfo{}, &SupervisorError{Code: CodeOperationPending, Message: "runtime operation is still in progress"}
			}
		}
	}
	if leaseID := m.requests[requestKey]; leaseID != "" {
		if existing := m.leases[leaseID]; existing != nil {
			if runtime := m.runtimeByIDLocked(existing.runtimeID); runtime != nil && existing.expiresAt.After(time.Now()) {
				info := runtimeInfo(runtime, existing.id)
				m.mu.Unlock()
				return info, nil
			}
		}
		delete(m.requests, requestKey)
	}
	m.mu.Unlock()

	runtime, err := m.ensureRuntime(ctx, clientID, requestID, params.GatewayIdentity, params.Profile, scope)
	if err != nil {
		return RuntimeInfo{}, err
	}
	newLease := &lease{id: randomID("lease"), runtimeID: runtime.record.RuntimeID, clientID: clientID, requestID: requestID, generation: runtime.record.Generation, expiresAt: time.Now().Add(m.cfg.SupervisorLeaseTTL())}
	m.mu.Lock()
	runtime.leases[newLease.id] = newLease
	runtime.record.LastLeaseAt = nowString()
	m.leases[newLease.id] = newLease
	m.requests[requestKey] = newLease.id
	if err := m.persistLocked(); err != nil {
		delete(runtime.leases, newLease.id)
		delete(m.leases, newLease.id)
		delete(m.requests, requestKey)
		m.mu.Unlock()
		return RuntimeInfo{}, registryError(err)
	}
	info := runtimeInfo(runtime, newLease.id)
	m.mu.Unlock()
	return info, nil
}

func (m *Manager) ensureRuntime(ctx context.Context, clientID, requestID, gatewayIdentity, profile, scope string) (*managedRuntime, error) {
	profile = strings.TrimSpace(profile)
	if gatewayIdentity == "" {
		gatewayIdentity = m.cfg.GatewayIdentity()
	}
	gatewayIdentity = normalizeGatewayIdentity(gatewayIdentity)
	// Validate before consulting the in-memory runtime map. A ready shared
	// runtime is deliberately keyed only by endpoint, so a later Acquire for a
	// different (or misspelled) Profile must not bypass the offline resolver.
	if profile != "" {
		if _, err := m.resolver.Exists(ctx, profile); err != nil {
			return nil, err
		}
	}
	key := runtimeKey(gatewayIdentity, profile, scope)
	m.mu.Lock()
	if runtime := m.runtimes[key]; runtime != nil && runtime.record.State == "READY" {
		m.mu.Unlock()
		if scope == "shared" && profile != "" {
			probeCfg := m.cfg
			setGatewayFromIdentity(&probeCfg, gatewayIdentity)
			if err := validateSharedProfile(ctx, probeCfg, profile); err != nil {
				return nil, err
			}
		}
		return runtime, nil
	} else if runtime := m.runtimes[key]; runtime != nil {
		state := runtime.record.State
		m.mu.Unlock()
		return nil, &SupervisorError{Code: CodeRuntimeUnavailable, Message: "runtime is " + strings.ToLower(state)}
	}
	m.mu.Unlock()

	probeCfg := m.cfg
	if gatewayIdentity != "" {
		setGatewayFromIdentity(&probeCfg, gatewayIdentity)
	}
	if scope == "shared" {
		if err := hermes.HTTPStatus(ctx, probeCfg); err == nil {
			if err := validateSharedProfile(ctx, probeCfg, profile); err != nil {
				return nil, err
			}
			runtime := &managedRuntime{record: registryRuntime{RuntimeID: randomID("runtime"), GatewayIdentity: gatewayIdentity, Profile: "", Scope: scope, GatewayURL: probeCfg.Gateway.URL, StatusURL: probeCfg.Gateway.StatusURL, Ownership: "adopted-shared", State: "READY", Generation: 1, SupervisorInstanceID: m.instanceID, LastHealthyAt: nowString()}, leases: make(map[string]*lease), pins: make(map[string]struct{}), pinOwners: make(map[string]string), pinGenerations: make(map[string]uint64), pinDeadlines: make(map[string]time.Time), ambiguousPins: make(map[string]bool), lastActivity: time.Now()}
			m.mu.Lock()
			m.runtimes[key] = runtime
			if err := m.persistLocked(); err != nil {
				delete(m.runtimes, key)
				m.mu.Unlock()
				return nil, registryError(err)
			}
			m.mu.Unlock()
			return runtime, nil
		}
	}
	port, err := endpointPort(probeCfg.Gateway.StatusURL)
	if err != nil {
		return nil, &SupervisorError{Code: CodeStartFailed, Message: "gateway endpoint has no usable port"}
	}
	if scope == "isolated" {
		port, err = allocatePort()
		if err != nil {
			return nil, &SupervisorError{Code: CodeStartFailed, Message: "allocate isolated gateway port"}
		}
	}
	host := endpointHost(probeCfg.Gateway.StatusURL)
	args := make([]string, 0, 12)
	if scope == "isolated" && profile != "" {
		args = append(args, "--profile", profile)
	}
	args = append(args, "serve", "--host", host, "--port", strconv.Itoa(port), "--no-open")
	if scope == "isolated" {
		args = append(args, "--isolated")
	}
	startCtx, cancel := context.WithTimeout(ctx, m.cfg.SupervisorStartTimeout())
	defer cancel()
	runtimeID := randomID("runtime")
	operationID := randomID("operation")
	launchNonce := randomID("launch")
	tokenRef, sessionToken, err := m.prepareRuntimeToken(runtimeID, "")
	if err != nil {
		return nil, &SupervisorError{Code: CodeStartFailed, Message: "could not prepare Hermes session token"}
	}
	managedCfg := probeCfg
	managedCfg.Gateway.StatusURL = fmt.Sprintf("http://%s:%d/api/status", host, port)
	managedCfg.Gateway.URL = fmt.Sprintf("ws://%s:%d/api/ws", host, port)
	managedCfg.Gateway.Token = ""
	managedCfg.Gateway.TokenFile = tokenRef
	ownership := "managed-shared"
	if scope == "isolated" {
		ownership = "managed-isolated"
	}
	runtimeProfile := profile
	if scope == "shared" {
		// A shared endpoint is not owned by one Profile. Profile routing is
		// carried in each Hermes RPC and the endpoint may serve many Profiles.
		runtimeProfile = ""
	}
	startedAt := nowString()
	environmentFingerprint := currentEnvironmentFingerprint(m.cfg.Supervisor.HermesExecutable)
	expectedCommand := commandFingerprint(append([]string{m.cfg.Supervisor.HermesExecutable}, args...))
	runtime := &managedRuntime{record: registryRuntime{
		RuntimeID: runtimeID, GatewayIdentity: gatewayIdentity, Profile: runtimeProfile, Scope: scope,
		GatewayURL: managedCfg.Gateway.URL, StatusURL: managedCfg.Gateway.StatusURL, TokenRef: tokenRef,
		Ownership: ownership, State: "STARTING", PID: 0, Generation: 1, CommandFingerprint: expectedCommand,
		Endpoint: managedCfg.GatewayIdentity(), LaunchNonce: launchNonce, OperationID: operationID, EnvironmentFingerprint: environmentFingerprint,
		SupervisorInstanceID: m.instanceID, StartedAt: startedAt, LastHealthyAt: "",
	}, leases: make(map[string]*lease), pins: make(map[string]struct{}), pinOwners: make(map[string]string), pinGenerations: make(map[string]uint64), pinDeadlines: make(map[string]time.Time), ambiguousPins: make(map[string]bool), lastActivity: time.Now()}
	operation := &registryOperation{
		OperationID: operationID, ClientID: clientID, RequestID: requestID, RuntimeID: runtimeID,
		GatewayIdentity: gatewayIdentity, Profile: profile, Scope: scope, GatewayURL: managedCfg.Gateway.URL,
		StatusURL: managedCfg.Gateway.StatusURL, TokenRef: tokenRef, Endpoint: managedCfg.GatewayIdentity(),
		LaunchNonce: launchNonce, EnvironmentFingerprint: environmentFingerprint, CommandFingerprint: expectedCommand, State: "STARTING",
		CreatedAt: startedAt, UpdatedAt: startedAt,
	}
	m.mu.Lock()
	m.runtimes[key] = runtime
	m.operations[clientID+"\x00"+requestID] = operation
	if err := m.persistLocked(); err != nil {
		delete(m.runtimes, key)
		delete(m.operations, clientID+"\x00"+requestID)
		m.mu.Unlock()
		_ = m.removeRuntimeToken(tokenRef)
		return nil, registryError(err)
	}
	m.mu.Unlock()

	process, err := startHermes(startCtx, m.cfg.Supervisor.HermesExecutable, args, sessionToken, runtimeID, launchNonce, managedCfg.GatewayIdentity())
	if err != nil {
		m.failStart(key, clientID+"\x00"+requestID, CodeStartFailed, "could not start Hermes Gateway", nil)
		_ = m.removeRuntimeToken(tokenRef)
		return nil, &SupervisorError{Code: CodeStartFailed, Message: "could not start Hermes Gateway"}
	}
	process.identity.Endpoint = managedCfg.GatewayIdentity()
	m.mu.Lock()
	runtime = m.runtimes[key]
	if runtime != nil {
		runtime.process = process
		runtime.record.PID = process.identity.PID
		runtime.record.ProcessStartToken = process.identity.ProcessStartToken
		runtime.record.ExecutablePath = process.identity.ExecutablePath
		runtime.record.CommandFingerprint = process.identity.CommandFingerprint
		runtime.record.Endpoint = process.identity.Endpoint
	}
	if operation = m.operations[clientID+"\x00"+requestID]; operation != nil {
		operation.PID = process.identity.PID
		operation.ProcessStartToken = process.identity.ProcessStartToken
		operation.ExecutablePath = process.identity.ExecutablePath
		operation.CommandFingerprint = process.identity.CommandFingerprint
		operation.UpdatedAt = nowString()
	}
	persistErr := m.persistLocked()
	m.mu.Unlock()
	if persistErr != nil {
		stopErr := process.stop(m.cfg.SupervisorShutdownGrace())
		m.failStart(key, clientID+"\x00"+requestID, CodeUnavailable, "could not persist started Hermes process identity", stopErr)
		if stopErr == nil {
			_ = m.removeRuntimeToken(tokenRef)
		}
		return nil, registryError(persistErr)
	}
	if err := waitReady(startCtx, managedCfg, &process.identity); err != nil {
		stopErr := process.stop(m.cfg.SupervisorShutdownGrace())
		code, message := supervisorErrorDetails(err, CodeStartTimeout)
		m.failStart(key, clientID+"\x00"+requestID, code, message, stopErr)
		if stopErr == nil {
			_ = m.removeRuntimeToken(tokenRef)
		}
		return nil, err
	}
	if scope == "shared" {
		if err := validateSharedProfile(startCtx, managedCfg, profile); err != nil {
			stopErr := process.stop(m.cfg.SupervisorShutdownGrace())
			code, message := supervisorErrorDetails(err, CodeSharedMultiplex)
			m.failStart(key, clientID+"\x00"+requestID, code, message, stopErr)
			if stopErr == nil {
				_ = m.removeRuntimeToken(tokenRef)
			}
			return nil, err
		}
	} else if profile != "" {
		client := hermes.NewClient(managedCfg)
		exists, profileErr := client.ProfileExists(startCtx, profile)
		client.Close()
		if profileErr != nil || !exists {
			stopErr := process.stop(m.cfg.SupervisorShutdownGrace())
			if profileErr != nil {
				failure := &SupervisorError{Code: CodeProfileLookup, Message: "runtime profile cross-check failed"}
				m.failStart(key, clientID+"\x00"+requestID, failure.Code, failure.Message, stopErr)
				if stopErr == nil {
					_ = m.removeRuntimeToken(tokenRef)
				}
				return nil, failure
			}
			failure := &SupervisorError{Code: CodeProfileNotFound, Message: fmt.Sprintf("Hermes profile %q does not exist", profile)}
			m.failStart(key, clientID+"\x00"+requestID, failure.Code, failure.Message, stopErr)
			if stopErr == nil {
				_ = m.removeRuntimeToken(tokenRef)
			}
			return nil, failure
		}
	}
	if !processIdentityCurrent(process.identity) {
		stopErr := process.stop(m.cfg.SupervisorShutdownGrace())
		m.failStart(key, clientID+"\x00"+requestID, CodeStartFailed, "Hermes process identity was lost before runtime became ready", stopErr)
		if stopErr == nil {
			_ = m.removeRuntimeToken(tokenRef)
		}
		return nil, &SupervisorError{Code: CodeStartFailed, Message: "Hermes process identity was lost before runtime became ready"}
	}
	m.mu.Lock()
	runtime = m.runtimes[key]
	if runtime == nil {
		m.mu.Unlock()
		_ = process.stop(m.cfg.SupervisorShutdownGrace())
		_ = m.removeRuntimeToken(tokenRef)
		return nil, &SupervisorError{Code: CodeRuntimeUnavailable, Message: "runtime operation disappeared during startup"}
	}
	runtime.record.State = "READY"
	runtime.record.StartedAt = nowString()
	runtime.record.LastHealthyAt = runtime.record.StartedAt
	if operation = m.operations[clientID+"\x00"+requestID]; operation != nil {
		operation.State = "READY"
		operation.UpdatedAt = runtime.record.LastHealthyAt
	}
	persistErr = m.persistLocked()
	m.mu.Unlock()
	if persistErr != nil {
		stopErr := process.stop(m.cfg.SupervisorShutdownGrace())
		m.failStart(key, clientID+"\x00"+requestID, CodeUnavailable, "could not persist ready Hermes runtime", stopErr)
		if stopErr == nil {
			_ = m.removeRuntimeToken(tokenRef)
		}
		return nil, registryError(persistErr)
	}
	return runtime, nil
}

func supervisorErrorDetails(err error, fallbackCode string) (string, string) {
	var coded *SupervisorError
	if errors.As(err, &coded) && coded != nil {
		return coded.Code, coded.Message
	}
	return fallbackCode, err.Error()
}

func (m *Manager) Heartbeat(_ context.Context, clientID, leaseID string, generation uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.leases[leaseID]
	if item == nil || item.clientID != clientID {
		return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "lease not found"}
	}
	if item.generation != generation {
		return &SupervisorError{Code: CodeGenerationChanged, Message: "lease generation is stale"}
	}
	if !item.expiresAt.After(time.Now()) {
		return &SupervisorError{Code: CodeLeaseExpired, Message: "lease has expired"}
	}
	item.expiresAt = time.Now().Add(m.cfg.SupervisorLeaseTTL())
	if runtime := m.runtimeByIDLocked(item.runtimeID); runtime != nil {
		runtime.record.LastLeaseAt = nowString()
		runtime.lastActivity = time.Now()
	}
	if err := m.persistLocked(); err != nil {
		return registryError(err)
	}
	return nil
}

func (m *Manager) failStart(runtimeKeyValue, requestKey, code, message string, stopErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if operation := m.operations[requestKey]; operation != nil {
		operation.State = "FAILED"
		operation.ErrorCode = code
		operation.ErrorMessage = message
		if stopErr != nil {
			operation.ErrorMessage += "; process ownership could not be safely stopped: " + stopErr.Error()
		}
		operation.UpdatedAt = nowString()
	}
	if runtime := m.runtimes[runtimeKeyValue]; runtime != nil {
		if stopErr == nil {
			delete(m.runtimes, runtimeKeyValue)
		} else {
			runtime.record.State = "FAILED"
		}
	}
	if err := m.persistLocked(); err != nil {
		m.log("runtime registry persist after failed startup failed: %v", err)
	}
}

func (m *Manager) Pin(_ context.Context, clientID, leaseID, sessionID string, generation uint64) error {
	if strings.TrimSpace(sessionID) == "" {
		return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "session_id is required"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.leases[leaseID]
	if item == nil || item.clientID != clientID {
		return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "lease not found"}
	}
	if item.generation != generation {
		return &SupervisorError{Code: CodeGenerationChanged, Message: "lease generation is stale"}
	}
	if !item.expiresAt.After(time.Now()) {
		return &SupervisorError{Code: CodeLeaseExpired, Message: "lease has expired"}
	}
	if runtime := m.runtimeByIDLocked(item.runtimeID); runtime != nil {
		if runtime.record.State != "READY" {
			return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "runtime is not ready"}
		}
		runtime.ensurePinMaps()
		pinKey := leaseID + "\x00" + sessionID
		runtime.pins[pinKey] = struct{}{}
		runtime.pinOwners[pinKey] = clientID
		runtime.pinGenerations[pinKey] = item.generation
		deadline := time.Now().Add(m.cfg.TurnTimeoutDuration())
		if deadline.Before(time.Now()) {
			deadline = time.Now().Add(2 * time.Hour)
		}
		runtime.pinDeadlines[pinKey] = deadline
		delete(runtime.ambiguousPins, pinKey)
		runtime.lastActivity = time.Now()
		if err := m.persistLocked(); err != nil {
			delete(runtime.pins, pinKey)
			delete(runtime.pinOwners, pinKey)
			delete(runtime.pinGenerations, pinKey)
			delete(runtime.pinDeadlines, pinKey)
			delete(runtime.ambiguousPins, pinKey)
			return registryError(err)
		}
		return nil
	}
	return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "runtime not found"}
}

func (m *Manager) Unpin(_ context.Context, clientID, leaseID, sessionID string, generation uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.leases[leaseID]
	if item == nil {
		pinKey := leaseID + "\x00" + sessionID
		for _, runtime := range m.runtimes {
			if _, ok := runtime.pins[pinKey]; ok {
				if owner := runtime.pinOwners[pinKey]; owner == "" || owner != clientID {
					return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "turn pin owner mismatch"}
				}
				if generation != 0 && runtime.pinGenerations[pinKey] != 0 && runtime.pinGenerations[pinKey] != generation {
					return &SupervisorError{Code: CodeGenerationChanged, Message: "turn pin generation is stale"}
				}
				delete(runtime.pins, pinKey)
				delete(runtime.pinOwners, pinKey)
				delete(runtime.pinGenerations, pinKey)
				delete(runtime.pinDeadlines, pinKey)
				delete(runtime.ambiguousPins, pinKey)
				return m.persistLocked()
			}
		}
		return nil
	}
	if item.clientID != clientID {
		return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "lease owner mismatch"}
	}
	if item.generation != generation {
		return &SupervisorError{Code: CodeGenerationChanged, Message: "lease generation is stale"}
	}
	var runtime *managedRuntime
	var hadPin bool
	var oldOwner string
	var oldGeneration uint64
	var oldDeadline time.Time
	var oldDeadlineSet bool
	var oldAmbiguous bool
	var oldAmbiguousSet bool
	if runtime = m.runtimeByIDLocked(item.runtimeID); runtime != nil {
		pinKey := leaseID + "\x00" + sessionID
		runtime.ensurePinMaps()
		_, hadPin = runtime.pins[pinKey]
		oldOwner, oldGeneration = runtime.pinOwners[pinKey], runtime.pinGenerations[pinKey]
		oldDeadline, oldDeadlineSet = runtime.pinDeadlines[pinKey]
		oldAmbiguous, oldAmbiguousSet = runtime.ambiguousPins[pinKey]
		delete(runtime.pins, pinKey)
		delete(runtime.pinOwners, pinKey)
		delete(runtime.pinGenerations, pinKey)
		delete(runtime.pinDeadlines, pinKey)
		delete(runtime.ambiguousPins, pinKey)
		runtime.lastActivity = time.Now()
	}
	if err := m.persistLocked(); err != nil {
		if runtime != nil && hadPin {
			pinKey := leaseID + "\x00" + sessionID
			runtime.pins[pinKey] = struct{}{}
			if oldOwner != "" {
				runtime.pinOwners[pinKey] = oldOwner
			}
			if oldGeneration != 0 {
				runtime.pinGenerations[pinKey] = oldGeneration
			}
			if oldDeadlineSet {
				runtime.pinDeadlines[pinKey] = oldDeadline
			}
			if oldAmbiguousSet {
				runtime.ambiguousPins[pinKey] = oldAmbiguous
			}
		}
		return registryError(err)
	}
	return nil
}

func (m *Manager) Release(_ context.Context, clientID, leaseID string, generation uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.leases[leaseID]
	if item == nil {
		return nil
	}
	if item.clientID != clientID {
		return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "lease owner mismatch"}
	}
	if item.generation != generation {
		return &SupervisorError{Code: CodeGenerationChanged, Message: "lease generation is stale"}
	}
	if runtime := m.runtimeByIDLocked(item.runtimeID); runtime != nil {
		for pinKey := range runtime.pins {
			if strings.HasPrefix(pinKey, leaseID+"\x00") {
				return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "lease has an active turn; unpin before release"}
			}
		}
		delete(runtime.leases, leaseID)
		delete(m.leases, leaseID)
		delete(m.requests, item.clientID+"\x00"+item.requestID)
		runtime.lastActivity = time.Now()
		if err := m.persistLocked(); err != nil {
			runtime.leases[leaseID] = item
			m.leases[leaseID] = item
			m.requests[item.clientID+"\x00"+item.requestID] = leaseID
			return registryError(err)
		}
		return nil
	}
	delete(m.leases, leaseID)
	delete(m.requests, item.clientID+"\x00"+item.requestID)
	if err := m.persistLocked(); err != nil {
		// Roll back the in-memory deletion so a later retry can persist it.
		m.leases[leaseID] = item
		m.requests[item.clientID+"\x00"+item.requestID] = leaseID
		if runtime := m.runtimeByIDLocked(item.runtimeID); runtime != nil {
			runtime.leases[leaseID] = item
		}
		return registryError(err)
	}
	return nil
}

func (m *Manager) Status(_ context.Context, runtimeID string) (RuntimeStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, runtime := range m.runtimes {
		if runtime.record.RuntimeID == runtimeID {
			return statusOf(runtime), nil
		}
	}
	return RuntimeStatus{}, &SupervisorError{Code: CodeRuntimeUnavailable, Message: "runtime not found"}
}

func (m *Manager) OperationStatus(_ context.Context, clientID string, params OperationParams) (OperationStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var operation *registryOperation
	if strings.TrimSpace(params.OperationID) != "" {
		for _, candidate := range m.operations {
			if candidate != nil && candidate.OperationID == strings.TrimSpace(params.OperationID) {
				operation = candidate
				break
			}
		}
	} else if strings.TrimSpace(params.RequestID) != "" {
		operation = m.operations[clientID+"\x00"+strings.TrimSpace(params.RequestID)]
	}
	if operation == nil {
		return OperationStatus{}, &SupervisorError{Code: CodeRuntimeUnavailable, Message: "runtime operation not found"}
	}
	return OperationStatus{
		OperationID:       operation.OperationID,
		ClientID:          operation.ClientID,
		RequestID:         operation.RequestID,
		RuntimeID:         operation.RuntimeID,
		State:             operation.State,
		PID:               operation.PID,
		ProcessStartToken: operation.ProcessStartToken,
		Endpoint:          operation.Endpoint,
		LaunchNonce:       operation.LaunchNonce,
		ErrorCode:         operation.ErrorCode,
		ErrorMessage:      operation.ErrorMessage,
		UpdatedAt:         operation.UpdatedAt,
	}, nil
}

func (m *Manager) Stop(_ context.Context, clientID, runtimeID string, generation uint64) error {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(runtimeID) == "" {
		return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "client_id and runtime_id are required"}
	}
	m.acquireMu.Lock()
	defer m.acquireMu.Unlock()
	m.mu.Lock()
	runtime := m.runtimeByIDLocked(runtimeID)
	if runtime == nil {
		m.mu.Unlock()
		// StopRuntime is a state-changing but idempotent IPC operation. A lost
		// response followed by a retry must not turn a successful stop into a
		// false failure once the registry has already removed the runtime.
		return nil
	}
	if runtime.record.Generation != generation {
		m.mu.Unlock()
		return &SupervisorError{Code: CodeGenerationChanged, Message: "runtime generation is stale"}
	}
	if runtime.record.Ownership == "adopted-shared" {
		m.mu.Unlock()
		return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "adopted runtime cannot be stopped by Supervisor"}
	}
	now := time.Now()
	for leaseID, item := range runtime.leases {
		if item.expiresAt.After(now) {
			continue
		}
		delete(runtime.leases, leaseID)
		delete(m.leases, leaseID)
		requestKey := item.clientID + "\x00" + item.requestID
		if m.requests[requestKey] == leaseID {
			delete(m.requests, requestKey)
		}
	}
	runtime.markExpiredPins(now)
	if len(runtime.leases) > 0 || runtime.hasActivePins() {
		m.mu.Unlock()
		return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "runtime has active leases or turns"}
	}
	runtime.clearAmbiguousPins()
	runtime.record.State = "DRAINING"
	runtime.record.State = "STOPPING"
	process := runtime.process
	record := runtime.record
	if record.LaunchNonce == "" {
		record.LaunchNonce = randomID("launch")
	}
	if err := m.persistLocked(); err != nil {
		m.mu.Unlock()
		return registryError(err)
	}
	m.mu.Unlock()
	if process == nil && record.PID > 0 {
		m.mu.Lock()
		if current := m.runtimeByIDLocked(runtimeID); current != nil {
			current.record.State = "DEGRADED"
		}
		_ = m.persistLocked()
		m.mu.Unlock()
		return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "runtime process identity is not proven"}
	}
	if process != nil {
		if err := process.stop(m.cfg.SupervisorShutdownGrace()); err != nil {
			m.mu.Lock()
			if current := m.runtimeByIDLocked(runtimeID); current != nil {
				current.record.State = "DEGRADED"
			}
			_ = m.persistLocked()
			m.mu.Unlock()
			return &SupervisorError{Code: CodeRuntimeUnavailable, Message: "runtime process did not stop"}
		}
	}
	m.mu.Lock()
	mapKey := ""
	for key, candidate := range m.runtimes {
		if candidate == runtime {
			mapKey = key
			break
		}
	}
	if mapKey == "" {
		mapKey = runtimeKey(record.GatewayIdentity, record.Profile, record.Scope)
	}
	delete(m.runtimes, mapKey)
	if err := m.persistLocked(); err != nil {
		m.mu.Unlock()
		return registryError(err)
	}
	m.mu.Unlock()
	if err := m.removeRuntimeToken(record.TokenRef); err != nil && !os.IsNotExist(err) {
		m.log("runtime token cleanup failed runtime_id=%s: %v", record.RuntimeID, err)
	}
	return nil
}

func (m *Manager) reapLoop() {
	defer m.reaperWG.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.reap()
		case <-m.stopReaper:
			return
		}
	}
}

var restartBackoff = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}

func (m *Manager) healthLoop() {
	defer m.healthWG.Done()
	interval := m.cfg.SupervisorHealthDuration()
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			m.checkHealth(now)
		case <-m.stopHealth:
			return
		}
	}
}

func (m *Manager) checkHealth(now time.Time) {
	type candidate struct {
		key       string
		status    string
		profile   string
		ownership string
		tokenRef  string
		identity  processIdentity
		adopted   bool
	}
	var candidates []candidate
	m.mu.Lock()
	for key, runtime := range m.runtimes {
		if runtime.record.State == "FAILED" {
			continue
		}
		if runtime.record.State == "RESTARTING" || runtime.record.State == "STOPPING" {
			continue
		}
		candidates = append(candidates, candidate{key: key, status: runtime.record.StatusURL, profile: runtime.record.Profile, ownership: runtime.record.Ownership, tokenRef: runtime.record.TokenRef, adopted: runtime.record.Ownership == "adopted-shared", identity: processIdentity{PID: runtime.record.PID, ProcessStartToken: runtime.record.ProcessStartToken, ExecutablePath: runtime.record.ExecutablePath, CommandFingerprint: runtime.record.CommandFingerprint, Endpoint: runtime.record.Endpoint, LaunchNonce: runtime.record.LaunchNonce}})
	}
	m.mu.Unlock()
	for _, item := range candidates {
		identityProven := item.adopted || (processAlive(item.identity.PID) && identityMatches(item.identity, currentProcessIdentity(item.identity.PID, item.identity.Endpoint)))
		healthy := identityProven
		if healthy {
			probeCfg := m.cfg
			probeCfg.Gateway.StatusURL = item.status
			if item.ownership != "adopted-shared" {
				probeCfg.Gateway.Token = ""
				probeCfg.Gateway.TokenFile = item.tokenRef
			}
			probeCtx, cancel := context.WithTimeout(context.Background(), m.cfg.ConnectTimeoutDuration())
			healthy = hermes.HTTPStatus(probeCtx, probeCfg) == nil
			cancel()
		}
		if healthy {
			m.markHealthy(item.key, now)
		} else if m.markUnhealthyCause(item.key, now, !identityProven) {
			m.restartRuntime(item.key)
		}
	}
}

func (m *Manager) markHealthy(key string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[key]
	if runtime == nil {
		return
	}
	runtime.healthFails = 0
	runtime.nextRestart = time.Time{}
	if runtime.record.State == "DEGRADED" {
		runtime.record.State = "READY"
	}
	runtime.record.LastHealthyAt = now.UTC().Format(time.RFC3339Nano)
	if operation := m.operationByID(runtime.record.OperationID); operation != nil && operation.State == "STARTING" {
		operation.State = "READY"
		operation.UpdatedAt = runtime.record.LastHealthyAt
	}
	if err := m.persistLocked(); err != nil {
		m.log("runtime registry persist during health update failed: %v", err)
	}
}

func (m *Manager) markUnhealthy(key string, now time.Time) bool {
	return m.markUnhealthyCause(key, now, true)
}

func (m *Manager) markUnhealthyCause(key string, now time.Time, definitive bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[key]
	if runtime == nil || runtime.record.State == "FAILED" {
		return false
	}
	if runtime.record.Ownership == "adopted-shared" {
		runtime.record.State = "DEGRADED"
		if err := m.persistLocked(); err != nil {
			m.log("runtime registry persist during adopted health failure failed: %v", err)
		}
		return false
	}
	runtime.record.State = "DEGRADED"
	runtime.markExpiredPins(now)
	if runtime.hasActivePins() {
		// An active turn is never interrupted by a lifecycle action. The next
		// health pass after UnpinTurn will schedule recovery.
		if err := m.persistLocked(); err != nil {
			m.log("runtime registry persist during degraded update failed: %v", err)
		}
		return false
	}
	runtime.clearAmbiguousPins()
	if now.Before(runtime.nextRestart) {
		return false
	}
	runtime.healthFails++
	if !definitive && runtime.healthFails < 2 {
		next := m.cfg.SupervisorHealthDuration()
		if next <= 0 {
			next = 5 * time.Second
		}
		runtime.nextRestart = now.Add(next)
		if err := m.persistLocked(); err != nil {
			m.log("runtime registry persist during transient health failure failed: %v", err)
		}
		return false
	}
	if runtime.healthFails > len(restartBackoff) {
		runtime.record.State = "FAILED"
		if err := m.persistLocked(); err != nil {
			m.log("runtime registry persist during failed update failed: %v", err)
		}
		return false
	}
	runtime.restarting = true
	runtime.record.State = "RESTARTING"
	if err := m.persistLocked(); err != nil {
		runtime.restarting = false
		runtime.record.State = "DEGRADED"
		m.log("runtime registry persist before restart failed: %v", err)
		return false
	}
	return true
}

func (m *Manager) restartRuntime(key string) {
	m.acquireMu.Lock()
	defer m.acquireMu.Unlock()
	m.mu.Lock()
	runtime := m.runtimes[key]
	if runtime == nil || !runtime.restarting || runtime.hasActivePins() || runtime.record.Ownership == "adopted-shared" {
		m.mu.Unlock()
		return
	}
	runtime.clearAmbiguousPins()
	record := runtime.record
	record.LaunchNonce = randomID("launch")
	oldProcess := runtime.process
	m.mu.Unlock()

	if oldProcess != nil {
		if err := oldProcess.stop(m.cfg.SupervisorShutdownGrace()); err != nil {
			// Keep the exact process identity when termination was not proven.
			// Starting a second Gateway on the same endpoint would violate the
			// ownership boundary and could strand the first process.
			m.finishRestartFailure(key, true)
			return
		}
	}
	startCtx, cancel := context.WithTimeout(context.Background(), m.cfg.SupervisorStartTimeout())
	process, managedCfg, tokenRef, err := m.startRuntimeProcess(startCtx, record)
	cancel()
	if err != nil {
		if process != nil {
			m.mu.Lock()
			if runtime := m.runtimes[key]; runtime != nil {
				runtime.process = process
				runtime.record.PID = process.identity.PID
				runtime.record.ProcessStartToken = process.identity.ProcessStartToken
				runtime.record.ExecutablePath = process.identity.ExecutablePath
				runtime.record.CommandFingerprint = process.identity.CommandFingerprint
				runtime.record.Endpoint = process.identity.Endpoint
				runtime.record.LaunchNonce = record.LaunchNonce
				runtime.record.TokenRef = tokenRef
				runtime.record.State = "FAILED"
				runtime.restarting = false
				if persistErr := m.persistLocked(); persistErr != nil {
					m.log("runtime registry persist after unsafe restart failure failed: %v", persistErr)
				}
			}
			m.mu.Unlock()
			return
		}
		m.finishRestartFailure(key, false)
		return
	}
	m.mu.Lock()
	runtime = m.runtimes[key]
	if runtime == nil {
		m.mu.Unlock()
		_ = process.stop(m.cfg.SupervisorShutdownGrace())
		return
	}
	runtime.process = process
	runtime.record.PID = process.identity.PID
	runtime.record.ProcessStartToken = process.identity.ProcessStartToken
	runtime.record.ExecutablePath = process.identity.ExecutablePath
	runtime.record.CommandFingerprint = process.identity.CommandFingerprint
	runtime.record.Endpoint = managedCfg.GatewayIdentity()
	runtime.record.GatewayURL = managedCfg.Gateway.URL
	runtime.record.StatusURL = managedCfg.Gateway.StatusURL
	runtime.record.TokenRef = tokenRef
	runtime.record.LaunchNonce = record.LaunchNonce
	runtime.record.Generation++
	runtime.record.State = "READY"
	runtime.record.StartedAt = nowString()
	runtime.record.LastHealthyAt = nowString()
	runtime.healthFails = 0
	runtime.nextRestart = time.Time{}
	runtime.restarting = false
	for _, lease := range runtime.leases {
		lease.generation = runtime.record.Generation
	}
	if err := m.persistLocked(); err != nil {
		m.log("runtime registry persist after restart failed: %v", err)
	}
	m.mu.Unlock()
}

func (m *Manager) finishRestartFailure(key string, keepProcess bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[key]
	if runtime == nil {
		return
	}
	if !keepProcess {
		runtime.process = nil
		runtime.record.PID = 0
		runtime.record.ProcessStartToken = ""
		runtime.record.CommandFingerprint = ""
	}
	runtime.record.State = "DEGRADED"
	runtime.restarting = false
	if keepProcess || runtime.healthFails >= len(restartBackoff) {
		runtime.record.State = "FAILED"
	} else {
		runtime.nextRestart = time.Now().Add(restartBackoff[runtime.healthFails])
	}
	if err := m.persistLocked(); err != nil {
		m.log("runtime registry persist after failed restart failed: %v", err)
	}
}

func (m *Manager) startRuntimeProcess(ctx context.Context, record registryRuntime) (*managedProcess, config.Config, string, error) {
	probeCfg := m.cfg
	setGatewayFromIdentity(&probeCfg, record.GatewayIdentity)
	host := endpointHost(record.StatusURL)
	port, err := endpointPort(record.StatusURL)
	if err != nil {
		return nil, config.Config{}, "", &SupervisorError{Code: CodeStartFailed, Message: "runtime endpoint has no usable port"}
	}
	args := processArgs(record)
	tokenRef, sessionToken, err := m.prepareRuntimeToken(record.RuntimeID, record.TokenRef)
	if err != nil {
		return nil, config.Config{}, "", &SupervisorError{Code: CodeStartFailed, Message: "could not prepare Hermes session token"}
	}
	process, err := startHermes(ctx, m.cfg.Supervisor.HermesExecutable, args, sessionToken, record.RuntimeID, record.LaunchNonce, record.Endpoint)
	if err != nil {
		_ = m.removeRuntimeToken(tokenRef)
		return nil, config.Config{}, "", &SupervisorError{Code: CodeStartFailed, Message: "could not start Hermes Gateway"}
	}
	managedCfg := probeCfg
	managedCfg.Gateway.StatusURL = fmt.Sprintf("http://%s:%d/api/status", host, port)
	managedCfg.Gateway.URL = fmt.Sprintf("ws://%s:%d/api/ws", host, port)
	managedCfg.Gateway.Token = ""
	managedCfg.Gateway.TokenFile = tokenRef
	process.identity.Endpoint = managedCfg.GatewayIdentity()
	if err := waitReady(ctx, managedCfg, &process.identity); err != nil {
		stopErr := process.stop(m.cfg.SupervisorShutdownGrace())
		if stopErr == nil {
			_ = m.removeRuntimeToken(tokenRef)
			return nil, config.Config{}, "", err
		}
		return process, managedCfg, tokenRef, fmt.Errorf("%w; process ownership could not be safely stopped: %v", err, stopErr)
	}
	return process, managedCfg, tokenRef, nil
}

func (m *Manager) reap() {
	now := time.Now()
	type candidate struct {
		key     string
		record  registryRuntime
		process *managedProcess
	}
	var candidates []candidate
	m.mu.Lock()
	for key, runtime := range m.runtimes {
		runtime.markExpiredPins(now)
		for id, item := range runtime.leases {
			if item.expiresAt.Before(now) {
				delete(runtime.leases, id)
				delete(m.leases, id)
				requestKey := item.clientID + "\x00" + item.requestID
				if m.requests[requestKey] == id {
					delete(m.requests, requestKey)
				}
			}
		}
		if runtime.record.State != "READY" || !shouldAutoReap(runtime, now, m.cfg.SupervisorIdleTimeout()) {
			continue
		}
		if runtime.process == nil && runtime.record.PID > 0 {
			// A restored runtime without a proven process handle must never be
			// deleted as if it had been stopped. Keep it adopted/stale for a
			// later explicit reconciliation instead of leaking an untracked
			// process behind a successful reap.
			continue
		}
		for pinKey := range runtime.pins {
			if runtime.ambiguousPins[pinKey] {
				delete(runtime.pins, pinKey)
				delete(runtime.pinOwners, pinKey)
				delete(runtime.pinGenerations, pinKey)
				delete(runtime.pinDeadlines, pinKey)
				delete(runtime.ambiguousPins, pinKey)
			}
		}
		runtime.reaping = true
		runtime.record.State = "STOPPING"
		candidates = append(candidates, candidate{key: key, record: runtime.record, process: runtime.process})
	}
	if err := m.persistLocked(); err != nil {
		m.log("runtime registry persist during reap failed: %v", err)
		for _, item := range candidates {
			if runtime := m.runtimes[item.key]; runtime != nil && runtime.reaping {
				runtime.reaping = false
				runtime.record.State = "READY"
			}
		}
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	for _, item := range candidates {
		if item.process != nil {
			if err := item.process.stop(m.cfg.SupervisorShutdownGrace()); err != nil {
				m.log("isolated runtime stop failed runtime_id=%s: %v", item.record.RuntimeID, err)
				m.finishReapFailure(item.key)
				continue
			}
		}
		m.mu.Lock()
		runtime := m.runtimes[item.key]
		if runtime == nil || !runtime.reaping {
			m.mu.Unlock()
			continue
		}
		if len(runtime.leases) > 0 || len(runtime.pins) > 0 {
			// A lease/turn appeared while the process was draining. Keep the
			// record visible and let the health monitor recover the Gateway.
			runtime.reaping = false
			runtime.record.State = "DEGRADED"
			runtime.record.PID = 0
			runtime.record.ProcessStartToken = ""
			runtime.record.CommandFingerprint = ""
			runtime.process = nil
			_ = m.persistLocked()
			m.mu.Unlock()
			continue
		}
		record := runtime.record
		delete(m.runtimes, item.key)
		if err := m.persistLocked(); err != nil {
			m.log("runtime registry persist after reap failed: %v", err)
		}
		m.mu.Unlock()
		if err := m.removeRuntimeToken(record.TokenRef); err != nil && !os.IsNotExist(err) {
			m.log("isolated runtime token cleanup failed runtime_id=%s: %v", record.RuntimeID, err)
		}
	}
}

func shouldAutoReap(runtime *managedRuntime, now time.Time, idleTimeout time.Duration) bool {
	return runtime != nil && !runtime.reaping && runtime.record.Ownership == "managed-isolated" && len(runtime.leases) == 0 && !runtime.hasActivePins() && now.Sub(runtime.lastActivity) >= idleTimeout
}

func (runtime *managedRuntime) ensurePinMaps() {
	if runtime.pinOwners == nil {
		runtime.pinOwners = make(map[string]string)
	}
	if runtime.pinGenerations == nil {
		runtime.pinGenerations = make(map[string]uint64)
	}
	if runtime.pinDeadlines == nil {
		runtime.pinDeadlines = make(map[string]time.Time)
	}
	if runtime.ambiguousPins == nil {
		runtime.ambiguousPins = make(map[string]bool)
	}
}

func (runtime *managedRuntime) clearAmbiguousPins() {
	if runtime == nil || runtime.ambiguousPins == nil {
		return
	}
	for pinKey, ambiguous := range runtime.ambiguousPins {
		if !ambiguous {
			continue
		}
		delete(runtime.pins, pinKey)
		delete(runtime.pinOwners, pinKey)
		delete(runtime.pinGenerations, pinKey)
		delete(runtime.pinDeadlines, pinKey)
		delete(runtime.ambiguousPins, pinKey)
	}
}

func (runtime *managedRuntime) markExpiredPins(now time.Time) {
	if runtime == nil {
		return
	}
	runtime.ensurePinMaps()
	for pinKey := range runtime.pins {
		deadline := runtime.pinDeadlines[pinKey]
		if !deadline.IsZero() && !now.Before(deadline) {
			runtime.ambiguousPins[pinKey] = true
		}
	}
}

func (runtime *managedRuntime) hasActivePins() bool {
	if runtime == nil {
		return false
	}
	for pinKey := range runtime.pins {
		if runtime.ambiguousPins == nil || !runtime.ambiguousPins[pinKey] {
			return true
		}
	}
	return false
}

func (m *Manager) finishReapFailure(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[key]
	if runtime == nil || !runtime.reaping {
		return
	}
	runtime.reaping = false
	runtime.record.State = "DEGRADED"
	if err := m.persistLocked(); err != nil {
		m.log("runtime registry persist after failed reap failed: %v", err)
	}
}

func (m *Manager) restore() error {
	runtimes, leases, pins, operations, err := m.registry.Load()
	if err != nil {
		m.log("runtime registry restore failed: %v", err)
		return fmt.Errorf("restore runtime registry: %w", err)
	}
	for _, item := range operations {
		value := item
		m.operations[item.ClientID+"\x00"+item.RequestID] = &value
	}
	for _, record := range runtimes {
		if record.Ownership == "adopted-shared" || record.State == "STOPPED" {
			continue
		}
		if record.State == "FAILED" && record.PID <= 0 {
			_ = m.removeRuntimeToken(record.TokenRef)
			continue
		}
		operation := m.operationByID(record.OperationID)
		environmentChanged := record.Ownership != "adopted-shared" && record.EnvironmentFingerprint != "" && record.EnvironmentFingerprint != currentEnvironmentFingerprint(m.cfg.Supervisor.HermesExecutable)
		if environmentChanged && operation != nil && operation.State == "STARTING" {
			operation.State = "FAILED"
			operation.ErrorCode = CodeRuntimeUnavailable
			operation.ErrorMessage = "Supervisor environment changed; old Hermes runtime was not resumed"
			operation.UpdatedAt = nowString()
		}
		if record.PID <= 0 && operation != nil && operation.State == "STARTING" {
			if actual, found := findProcessByLaunchNonce(record.LaunchNonce, record.Endpoint, record.ExecutablePath); found {
				record.PID = actual.PID
				record.ProcessStartToken = actual.ProcessStartToken
				record.ExecutablePath = actual.ExecutablePath
				record.CommandFingerprint = actual.CommandFingerprint
				record.Endpoint = actual.Endpoint
				operation.PID = actual.PID
				operation.ProcessStartToken = actual.ProcessStartToken
				operation.ExecutablePath = actual.ExecutablePath
				operation.CommandFingerprint = actual.CommandFingerprint
				operation.UpdatedAt = nowString()
			}
		}
		if !processAlive(record.PID) {
			if record.State == "STOPPING" || record.State == "STARTING" || record.State == "FAILED" {
				_ = m.removeRuntimeToken(record.TokenRef)
			}
			if operation := m.operationByID(record.OperationID); operation != nil && operation.State == "STARTING" {
				operation.State = "FAILED"
				operation.ErrorCode = CodeRuntimeUnavailable
				operation.ErrorMessage = "Supervisor restarted without a proven Hermes process identity"
				operation.UpdatedAt = nowString()
			}
			continue
		}
		actual := currentProcessIdentity(record.PID, record.Endpoint)
		expected := processIdentity{PID: record.PID, ProcessStartToken: record.ProcessStartToken, ExecutablePath: record.ExecutablePath, CommandFingerprint: record.CommandFingerprint, Endpoint: record.Endpoint, LaunchNonce: record.LaunchNonce}
		var process *managedProcess
		if identityMatches(expected, actual) {
			process = reattachedProcess(actual, processArgs(record))
			if operation := m.operationByID(record.OperationID); operation != nil {
				operation.PID = actual.PID
				operation.ProcessStartToken = actual.ProcessStartToken
				operation.ExecutablePath = actual.ExecutablePath
				operation.CommandFingerprint = actual.CommandFingerprint
				operation.UpdatedAt = nowString()
			}
			if environmentChanged {
				record.State = "FAILED"
			} else if record.State == "STOPPING" {
				// A service-manager restart interrupted an explicit stop or idle
				// reap. Keep the proven process visible but fail closed; a later
				// explicit StopRuntime can finish the exact shutdown.
				record.State = "FAILED"
			} else if record.State == "FAILED" {
				// Keep a proven failed process addressable for explicit diagnostics
				// and StopRuntime; FAILED runtimes are never auto-restarted.
			} else if record.State != "READY" {
				record.State = "DEGRADED"
			}
		} else {
			// Without a complete proof, never kill or restart the process.
			if operation := m.operationByID(record.OperationID); operation != nil && operation.State == "STARTING" {
				operation.State = "FAILED"
				operation.ErrorCode = CodeRuntimeUnavailable
				operation.ErrorMessage = "Hermes process identity could not be proven after Supervisor restart"
				operation.UpdatedAt = nowString()
			}
			record.Ownership = "adopted-shared"
			record.Scope = "shared"
			record.Profile = ""
			record.TokenRef = "local-config"
			record.State = "READY"
		}
		record.SupervisorInstanceID = m.instanceID
		runtime := &managedRuntime{record: record, process: process, leases: make(map[string]*lease), pins: make(map[string]struct{}), pinOwners: make(map[string]string), pinGenerations: make(map[string]uint64), pinDeadlines: make(map[string]time.Time), ambiguousPins: make(map[string]bool), lastActivity: time.Now()}
		key := runtimeKey(record.GatewayIdentity, record.Profile, record.Scope)
		if environmentChanged {
			// Keep the stale runtime addressable by RuntimeID for explicit
			// diagnostics/stop, but do not let it block a new runtime on the same
			// endpoint under the new Supervisor environment.
			key += "\x00stale\x00" + record.RuntimeID
		}
		m.runtimes[key] = runtime
	}
	for _, operation := range m.operations {
		if operation == nil || operation.State != "STARTING" {
			continue
		}
		if m.runtimeByIDLocked(operation.RuntimeID) == nil {
			operation.State = "FAILED"
			operation.ErrorCode = CodeRuntimeUnavailable
			operation.ErrorMessage = "Supervisor restarted without a recoverable runtime record"
			operation.UpdatedAt = nowString()
		}
	}
	for _, item := range leases {
		if item.ExpiresAt.Before(time.Now()) {
			continue
		}
		runtime := m.runtimeByIDLocked(item.RuntimeID)
		if runtime == nil {
			continue
		}
		value := &lease{id: item.LeaseID, runtimeID: item.RuntimeID, clientID: item.ClientID, requestID: item.RequestID, generation: item.Generation, expiresAt: item.ExpiresAt}
		runtime.leases[value.id] = value
		m.leases[value.id] = value
		m.requests[value.clientID+"\x00"+value.requestID] = value.id
	}
	for _, item := range pins {
		runtime := m.runtimeByIDLocked(item.RuntimeID)
		lease := m.leases[item.LeaseID]
		if runtime == nil {
			continue
		}
		generation := item.Generation
		if lease != nil {
			if lease.generation != item.Generation || lease.runtimeID != item.RuntimeID {
				continue
			}
			generation = lease.generation
		} else if item.TurnDeadline == "" {
			item.TurnDeadline = time.Now().Add(m.cfg.TurnTimeoutDuration()).UTC().Format(time.RFC3339Nano)
		} else if deadline, parseErr := time.Parse(time.RFC3339Nano, item.TurnDeadline); parseErr != nil || !deadline.After(time.Now()) {
			continue
		}
		if item.TurnDeadline == "" {
			item.TurnDeadline = time.Now().Add(m.cfg.TurnTimeoutDuration()).UTC().Format(time.RFC3339Nano)
		}
		pinKey := item.LeaseID + "\x00" + item.SessionID
		runtime.pins[pinKey] = struct{}{}
		runtime.ensurePinMaps()
		runtime.pinGenerations[pinKey] = generation
		if item.ClientID != "" {
			runtime.pinOwners[pinKey] = item.ClientID
		} else if lease != nil {
			runtime.pinOwners[pinKey] = lease.clientID
		}
		if item.TurnDeadline != "" {
			if deadline, parseErr := time.Parse(time.RFC3339Nano, item.TurnDeadline); parseErr == nil {
				runtime.pinDeadlines[pinKey] = deadline
			}
		}
		if item.State == "AMBIGUOUS" {
			runtime.ambiguousPins[pinKey] = true
		}
	}
	if err := m.persistLocked(); err != nil {
		m.log("runtime registry restore persist failed: %v", err)
		return fmt.Errorf("persist restored runtime registry: %w", err)
	}
	return nil
}

func (m *Manager) persistLocked() error {
	value := make([]registryRuntime, 0, len(m.runtimes))
	for _, runtime := range m.runtimes {
		value = append(value, runtime.record)
	}
	leaseValues := make([]registryLease, 0, len(m.leases))
	for _, item := range m.leases {
		leaseValues = append(leaseValues, registryLease{LeaseID: item.id, RuntimeID: item.runtimeID, ClientID: item.clientID, RequestID: item.requestID, Generation: item.generation, ExpiresAt: item.expiresAt})
	}
	pinValues := make([]registryPin, 0)
	for _, runtime := range m.runtimes {
		for pinKey := range runtime.pins {
			parts := strings.SplitN(pinKey, "\x00", 2)
			if len(parts) != 2 {
				continue
			}
			lease := m.leases[parts[0]]
			generation := runtime.record.Generation
			if lease != nil {
				generation = lease.generation
			}
			state := "ACTIVE"
			if runtime.ambiguousPins != nil && runtime.ambiguousPins[pinKey] {
				state = "AMBIGUOUS"
			}
			deadline := ""
			if runtime.pinDeadlines != nil {
				if value, ok := runtime.pinDeadlines[pinKey]; ok && !value.IsZero() {
					deadline = value.UTC().Format(time.RFC3339Nano)
				}
			}
			pinValues = append(pinValues, registryPin{ClientID: runtime.pinOwners[pinKey], LeaseID: parts[0], RuntimeID: runtime.record.RuntimeID, SessionID: parts[1], Generation: generation, TurnDeadline: deadline, State: state})
		}
	}
	operationValues := make([]registryOperation, 0, len(m.operations))
	for _, operation := range m.operations {
		if operation != nil {
			operationValues = append(operationValues, *operation)
		}
	}
	return m.registry.Save(value, leaseValues, pinValues, operationValues)
}

func (m *Manager) runtimeByIDLocked(runtimeID string) *managedRuntime {
	for _, runtime := range m.runtimes {
		if runtime.record.RuntimeID == runtimeID {
			return runtime
		}
	}
	return nil
}

func (m *Manager) operationByID(operationID string) *registryOperation {
	if strings.TrimSpace(operationID) == "" {
		return nil
	}
	for _, operation := range m.operations {
		if operation != nil && operation.OperationID == operationID {
			return operation
		}
	}
	return nil
}

func runtimeInfo(runtime *managedRuntime, leaseID string) RuntimeInfo {
	tokenRef := runtime.record.TokenRef
	if strings.TrimSpace(tokenRef) == "" {
		tokenRef = "local-config"
	}
	return RuntimeInfo{RuntimeID: runtime.record.RuntimeID, LeaseID: leaseID, Generation: runtime.record.Generation, Ownership: runtime.record.Ownership, GatewayURL: runtime.record.GatewayURL, StatusURL: runtime.record.StatusURL, TokenRef: tokenRef, OperationID: runtime.record.OperationID}
}

func registryError(err error) error {
	if err == nil {
		return nil
	}
	return &SupervisorError{Code: CodeUnavailable, Message: "Supervisor runtime registry is unavailable"}
}

func normalizeGatewayIdentity(raw string) string {
	raw = strings.TrimSpace(raw)
	value, err := url.Parse(raw)
	if err != nil || value.Scheme == "" || value.Host == "" {
		return raw
	}
	value.RawQuery = ""
	value.Fragment = ""
	value.Path = strings.TrimRight(value.Path, "/")
	return value.String()
}

func runtimeKey(gatewayIdentity, profile, scope string) string {
	key := normalizeGatewayIdentity(gatewayIdentity)
	if scope == "isolated" {
		key += "\x00" + strings.TrimSpace(profile)
	}
	return key
}

func statusOf(runtime *managedRuntime) RuntimeStatus {
	return RuntimeStatus{RuntimeInfo: runtimeInfo(runtime, ""), State: runtime.record.State, PID: runtime.record.PID, LeaseCount: len(runtime.leases), PinCount: len(runtime.pins), IdentityOK: runtime.process != nil || runtime.record.Ownership == "adopted-shared", LastHealthy: runtime.record.LastHealthyAt}
}

func (m *Manager) runtimeTokenPath(runtimeID string) string {
	return filepath.Join(filepath.Dir(m.cfg.SupervisorRuntimeDB()), "tokens", runtimeID+".token")
}

func (m *Manager) prepareRuntimeToken(runtimeID, _ string) (string, string, error) {
	ref := m.runtimeTokenPath(runtimeID)
	if err := os.MkdirAll(filepath.Dir(ref), 0o700); err != nil {
		return "", "", err
	}
	if info, err := os.Lstat(ref); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("runtime token file is a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return "", "", err
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(bytes)
	file, err := os.OpenFile(ref, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", err
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		return "", "", err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", "", err
	}
	if err := file.Close(); err != nil {
		return "", "", err
	}
	return ref, token, nil
}

func (m *Manager) removeRuntimeToken(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "local-config" {
		return nil
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(m.cfg.SupervisorRuntimeDB()), "tokens"))
	clean := filepath.Clean(ref)
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return nil
	}
	return os.Remove(clean)
}

func randomID(prefix string) string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return prefix + "-" + hex.EncodeToString(bytes)
}

func currentEnvironmentFingerprint(executable string) string {
	resolved := strings.TrimSpace(executable)
	if path, err := exec.LookPath(resolved); err == nil {
		resolved = path
	}
	return commandFingerprint([]string{
		normalizeExecutablePath(resolved),
		os.Getenv("HOME"),
		os.Getenv("HERMES_HOME"),
	})
}

func filepathDir(path string) string {
	index := strings.LastIndexAny(path, `/\\`)
	if index < 0 {
		return "."
	}
	return path[:index]
}
func endpointHost(raw string) string {
	value, err := url.Parse(raw)
	if err != nil || value.Hostname() == "" {
		return "127.0.0.1"
	}
	return value.Hostname()
}
func endpointPort(raw string) (int, error) {
	value, err := url.Parse(raw)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(value.Port())
	if err != nil || port <= 0 {
		return 0, fmt.Errorf("invalid endpoint port")
	}
	return port, nil
}
func allocatePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
func setGatewayFromIdentity(cfg *config.Config, identity string) {
	value, err := url.Parse(identity)
	if err != nil {
		return
	}
	if value.Scheme == "ws" || value.Scheme == "wss" {
		cfg.Gateway.URL = value.String()
		cfg.Gateway.StatusURL = strings.Replace(value.String(), "ws://", "http://", 1)
		cfg.Gateway.StatusURL = strings.Replace(cfg.Gateway.StatusURL, "wss://", "https://", 1)
		if parsed, err := url.Parse(cfg.Gateway.StatusURL); err == nil {
			parsed.Path = "/api/status"
			parsed.RawQuery = ""
			cfg.Gateway.StatusURL = parsed.String()
		}
	}
}

func validateSharedProfile(ctx context.Context, cfg config.Config, profile string) error {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return nil
	}
	client := hermes.NewClient(cfg)
	defer client.Close()
	topology, err := client.ProfileTopology(ctx)
	if err != nil {
		return &SupervisorError{Code: CodeSharedMultiplex, Message: "cannot verify Hermes shared Profile multiplex topology"}
	}
	if topology.GatewayMode != "multiplex" {
		return &SupervisorError{Code: CodeSharedMultiplex, Message: "shared Gateway must have Hermes multiplex_profiles enabled"}
	}
	if !topology.Serves(profile) {
		for _, item := range topology.Profiles {
			if strings.TrimSpace(item.Name) == profile {
				return &SupervisorError{Code: CodeProfileNotServed, Message: fmt.Sprintf("Hermes Profile %q exists but is not served by the shared Gateway", profile)}
			}
		}
		return &SupervisorError{Code: CodeProfileNotFound, Message: fmt.Sprintf("Hermes shared Gateway does not serve Profile %q", profile)}
	}
	return nil
}

func waitReady(ctx context.Context, cfg config.Config, identity *processIdentity) error {
	deadline := time.NewTimer(cfg.SupervisorStartTimeout())
	defer deadline.Stop()
	for {
		if identity != nil && !processIdentityCurrent(*identity) {
			return &SupervisorError{Code: CodeStartFailed, Message: "Hermes process identity is not proven for the configured endpoint"}
		}
		probeCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeoutDuration())
		statusErr := hermes.HTTPStatus(probeCtx, cfg)
		if statusErr == nil {
			client := hermes.NewClient(cfg)
			connectErr := client.Connect(probeCtx)
			client.Close()
			cancel()
			if connectErr == nil {
				return nil
			}
		} else {
			cancel()
		}
		select {
		case <-ctx.Done():
			return &SupervisorError{Code: CodeStartTimeout, Message: "Hermes Gateway did not become ready"}
		case <-deadline.C:
			return &SupervisorError{Code: CodeStartTimeout, Message: "Hermes Gateway did not become ready"}
		case <-time.After(200 * time.Millisecond):
		}
	}
}

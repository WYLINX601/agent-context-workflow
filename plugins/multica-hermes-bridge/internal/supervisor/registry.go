package supervisor

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
)

type registryRuntime struct {
	RuntimeID              string
	GatewayIdentity        string
	Profile                string
	Scope                  string
	GatewayURL             string
	StatusURL              string
	TokenRef               string
	Ownership              string
	State                  string
	PID                    int
	ProcessStartToken      string
	ExecutablePath         string
	Generation             uint64
	CommandFingerprint     string
	Endpoint               string
	LaunchNonce            string
	OperationID            string
	EnvironmentFingerprint string
	SupervisorInstanceID   string
	StartedAt              string
	LastHealthyAt          string
	LastLeaseAt            string
}

type registryLease struct {
	LeaseID    string
	RuntimeID  string
	ClientID   string
	RequestID  string
	Generation uint64
	ExpiresAt  time.Time
}

type registryPin struct {
	ClientID     string
	LeaseID      string
	RuntimeID    string
	SessionID    string
	Generation   uint64
	TurnDeadline string
	State        string
}

type registryOperation struct {
	OperationID            string
	ClientID               string
	RequestID              string
	RuntimeID              string
	GatewayIdentity        string
	Profile                string
	Scope                  string
	GatewayURL             string
	StatusURL              string
	TokenRef               string
	Endpoint               string
	LaunchNonce            string
	EnvironmentFingerprint string
	PID                    int
	ProcessStartToken      string
	ExecutablePath         string
	CommandFingerprint     string
	State                  string
	ErrorCode              string
	ErrorMessage           string
	CreatedAt              string
	UpdatedAt              string
}

type registryDB struct {
	db   *sql.DB
	lock *flock.Flock
}

func openRegistry(path string) (*registryDB, error) {
	if path == "" {
		return nil, fmt.Errorf("runtime registry path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create runtime registry directory: %w", err)
	}
	lock := flock.New(path + ".lock")
	lockCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	locked, lockErr := lock.TryLockContext(lockCtx, 25*time.Millisecond)
	cancel()
	if lockErr != nil {
		return nil, fmt.Errorf("acquire runtime registry lock: %w", lockErr)
	}
	if !locked {
		return nil, fmt.Errorf("runtime registry is already owned by another Supervisor")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		_ = lock.Unlock()
		return nil, fmt.Errorf("open runtime registry: %w", err)
	}
	db.SetMaxOpenConns(1)
	registry := &registryDB{db: db, lock: lock}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS supervisor_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS runtimes (
 runtime_id TEXT PRIMARY KEY,
 gateway_identity TEXT NOT NULL,
 profile TEXT NOT NULL,
 scope TEXT NOT NULL,
	gateway_url TEXT NOT NULL,
	status_url TEXT NOT NULL,
	token_ref TEXT NOT NULL DEFAULT '',
	ownership TEXT NOT NULL,
	state TEXT NOT NULL,
	pid INTEGER NOT NULL,
	process_start_token TEXT NOT NULL,
	executable_path TEXT NOT NULL DEFAULT '',
	generation INTEGER NOT NULL,
	command_fingerprint TEXT NOT NULL,
	endpoint TEXT NOT NULL,
	launch_nonce TEXT NOT NULL DEFAULT '',
	operation_id TEXT NOT NULL DEFAULT '',
	environment_fingerprint TEXT NOT NULL DEFAULT '',
	supervisor_instance_id TEXT NOT NULL,
 started_at TEXT NOT NULL,
 last_healthy_at TEXT NOT NULL,
 last_lease_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS leases (
 lease_id TEXT PRIMARY KEY,
 runtime_id TEXT NOT NULL,
 client_id TEXT NOT NULL,
 request_id TEXT NOT NULL,
 generation INTEGER NOT NULL,
	 expires_at TEXT NOT NULL,
	 UNIQUE(client_id, request_id)
);
CREATE TABLE IF NOT EXISTS turn_pins (
	 client_id TEXT NOT NULL DEFAULT '',
	 lease_id TEXT NOT NULL,
	 runtime_id TEXT NOT NULL,
	 session_id TEXT NOT NULL,
	 generation INTEGER NOT NULL,
	 turn_deadline TEXT NOT NULL DEFAULT '',
	 state TEXT NOT NULL DEFAULT 'ACTIVE',
	 PRIMARY KEY(lease_id, session_id)
	);
CREATE TABLE IF NOT EXISTS runtime_operations (
	 operation_id TEXT PRIMARY KEY,
	 client_id TEXT NOT NULL,
	 request_id TEXT NOT NULL,
	 runtime_id TEXT NOT NULL,
	 gateway_identity TEXT NOT NULL,
	 profile TEXT NOT NULL,
	 scope TEXT NOT NULL,
	 gateway_url TEXT NOT NULL,
	 status_url TEXT NOT NULL,
	 token_ref TEXT NOT NULL DEFAULT '',
	 endpoint TEXT NOT NULL,
	 launch_nonce TEXT NOT NULL,
	 environment_fingerprint TEXT NOT NULL DEFAULT '',
	 pid INTEGER NOT NULL,
	 process_start_token TEXT NOT NULL,
	 executable_path TEXT NOT NULL DEFAULT '',
	 command_fingerprint TEXT NOT NULL,
	 state TEXT NOT NULL,
	 error_code TEXT NOT NULL DEFAULT '',
	 error_message TEXT NOT NULL DEFAULT '',
	 created_at TEXT NOT NULL,
	 updated_at TEXT NOT NULL,
	 UNIQUE(client_id, request_id)
);`); err != nil {
		_ = db.Close()
		_ = lock.Unlock()
		return nil, fmt.Errorf("initialize runtime registry: %w", err)
	}
	for _, migration := range []struct {
		table string
		name  string
		def   string
	}{
		{"runtimes", "token_ref", "TEXT NOT NULL DEFAULT ''"},
		{"runtimes", "executable_path", "TEXT NOT NULL DEFAULT ''"},
		{"runtimes", "launch_nonce", "TEXT NOT NULL DEFAULT ''"},
		{"runtimes", "operation_id", "TEXT NOT NULL DEFAULT ''"},
		{"runtimes", "environment_fingerprint", "TEXT NOT NULL DEFAULT ''"},
		{"turn_pins", "turn_deadline", "TEXT NOT NULL DEFAULT ''"},
		{"turn_pins", "state", "TEXT NOT NULL DEFAULT 'ACTIVE'"},
		{"turn_pins", "client_id", "TEXT NOT NULL DEFAULT ''"},
		{"runtime_operations", "environment_fingerprint", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addRegistryColumn(db, migration.table, migration.name, migration.def); err != nil {
			_ = db.Close()
			_ = lock.Unlock()
			return nil, fmt.Errorf("upgrade runtime registry schema: %w", err)
		}
	}
	return registry, nil
}

func addRegistryColumn(db *sql.DB, table, wanted, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == wanted {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + wanted + ` ` + definition)
	return err
}

func (r *registryDB) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	dbErr := r.db.Close()
	if r.lock == nil {
		return dbErr
	}
	if lockErr := r.lock.Unlock(); dbErr != nil && lockErr != nil {
		return fmt.Errorf("close runtime registry: %v; release registry lock: %w", dbErr, lockErr)
	} else if dbErr != nil {
		return dbErr
	} else {
		return lockErr
	}
}

func (r *registryDB) Load() ([]registryRuntime, []registryLease, []registryPin, []registryOperation, error) {
	runtimeRows, err := r.db.Query(`SELECT runtime_id, gateway_identity, profile, scope, gateway_url, status_url, token_ref, ownership, state, pid, process_start_token, executable_path, generation, command_fingerprint, endpoint, launch_nonce, operation_id, environment_fingerprint, supervisor_instance_id, started_at, last_healthy_at, last_lease_at FROM runtimes`)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer runtimeRows.Close()
	var runtimes []registryRuntime
	for runtimeRows.Next() {
		var value registryRuntime
		if err := runtimeRows.Scan(&value.RuntimeID, &value.GatewayIdentity, &value.Profile, &value.Scope, &value.GatewayURL, &value.StatusURL, &value.TokenRef, &value.Ownership, &value.State, &value.PID, &value.ProcessStartToken, &value.ExecutablePath, &value.Generation, &value.CommandFingerprint, &value.Endpoint, &value.LaunchNonce, &value.OperationID, &value.EnvironmentFingerprint, &value.SupervisorInstanceID, &value.StartedAt, &value.LastHealthyAt, &value.LastLeaseAt); err != nil {
			return nil, nil, nil, nil, err
		}
		runtimes = append(runtimes, value)
	}
	if err := runtimeRows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	if err := runtimeRows.Close(); err != nil {
		return nil, nil, nil, nil, err
	}
	leaseRows, err := r.db.Query(`SELECT lease_id, runtime_id, client_id, request_id, generation, expires_at FROM leases`)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer leaseRows.Close()
	var leases []registryLease
	for leaseRows.Next() {
		var value registryLease
		var expires string
		if err := leaseRows.Scan(&value.LeaseID, &value.RuntimeID, &value.ClientID, &value.RequestID, &value.Generation, &expires); err != nil {
			return nil, nil, nil, nil, err
		}
		value.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			continue
		}
		leases = append(leases, value)
	}
	if err := leaseRows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	if err := leaseRows.Close(); err != nil {
		return nil, nil, nil, nil, err
	}
	pinRows, err := r.db.Query(`SELECT client_id, lease_id, runtime_id, session_id, generation, turn_deadline, state FROM turn_pins`)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer pinRows.Close()
	var pins []registryPin
	for pinRows.Next() {
		var value registryPin
		if err := pinRows.Scan(&value.ClientID, &value.LeaseID, &value.RuntimeID, &value.SessionID, &value.Generation, &value.TurnDeadline, &value.State); err != nil {
			return nil, nil, nil, nil, err
		}
		pins = append(pins, value)
	}
	if err := pinRows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	if err := pinRows.Close(); err != nil {
		return nil, nil, nil, nil, err
	}
	operations, err := r.loadOperations()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return runtimes, leases, pins, operations, nil
}

func (r *registryDB) loadOperations() ([]registryOperation, error) {
	rows, err := r.db.Query(`SELECT operation_id, client_id, request_id, runtime_id, gateway_identity, profile, scope, gateway_url, status_url, token_ref, endpoint, launch_nonce, environment_fingerprint, pid, process_start_token, executable_path, command_fingerprint, state, error_code, error_message, created_at, updated_at FROM runtime_operations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []registryOperation
	for rows.Next() {
		var value registryOperation
		if err := rows.Scan(&value.OperationID, &value.ClientID, &value.RequestID, &value.RuntimeID, &value.GatewayIdentity, &value.Profile, &value.Scope, &value.GatewayURL, &value.StatusURL, &value.TokenRef, &value.Endpoint, &value.LaunchNonce, &value.EnvironmentFingerprint, &value.PID, &value.ProcessStartToken, &value.ExecutablePath, &value.CommandFingerprint, &value.State, &value.ErrorCode, &value.ErrorMessage, &value.CreatedAt, &value.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (r *registryDB) Save(runtimes []registryRuntime, leases []registryLease, pins []registryPin, operations []registryOperation) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM leases`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM runtimes`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM turn_pins`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM runtime_operations`); err != nil {
		return err
	}
	for _, value := range runtimes {
		_, err := tx.Exec(`INSERT INTO runtimes(runtime_id, gateway_identity, profile, scope, gateway_url, status_url, token_ref, ownership, state, pid, process_start_token, executable_path, generation, command_fingerprint, endpoint, launch_nonce, operation_id, environment_fingerprint, supervisor_instance_id, started_at, last_healthy_at, last_lease_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.RuntimeID, value.GatewayIdentity, value.Profile, value.Scope, value.GatewayURL, value.StatusURL, value.TokenRef, value.Ownership, value.State, value.PID, value.ProcessStartToken, value.ExecutablePath, value.Generation, value.CommandFingerprint, value.Endpoint, value.LaunchNonce, value.OperationID, value.EnvironmentFingerprint, value.SupervisorInstanceID, value.StartedAt, value.LastHealthyAt, value.LastLeaseAt)
		if err != nil {
			return err
		}
	}
	for _, value := range leases {
		_, err := tx.Exec(`INSERT INTO leases(lease_id, runtime_id, client_id, request_id, generation, expires_at) VALUES(?,?,?,?,?,?)`, value.LeaseID, value.RuntimeID, value.ClientID, value.RequestID, value.Generation, value.ExpiresAt.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	for _, value := range pins {
		_, err := tx.Exec(`INSERT INTO turn_pins(client_id, lease_id, runtime_id, session_id, generation, turn_deadline, state) VALUES(?,?,?,?,?,?,?)`, value.ClientID, value.LeaseID, value.RuntimeID, value.SessionID, value.Generation, value.TurnDeadline, value.State)
		if err != nil {
			return err
		}
	}
	for _, value := range operations {
		_, err := tx.Exec(`INSERT INTO runtime_operations(operation_id, client_id, request_id, runtime_id, gateway_identity, profile, scope, gateway_url, status_url, token_ref, endpoint, launch_nonce, environment_fingerprint, pid, process_start_token, executable_path, command_fingerprint, state, error_code, error_message, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.OperationID, value.ClientID, value.RequestID, value.RuntimeID, value.GatewayIdentity, value.Profile, value.Scope, value.GatewayURL, value.StatusURL, value.TokenRef, value.Endpoint, value.LaunchNonce, value.EnvironmentFingerprint, value.PID, value.ProcessStartToken, value.ExecutablePath, value.CommandFingerprint, value.State, value.ErrorCode, value.ErrorMessage, value.CreatedAt, value.UpdatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nowString() string { return time.Now().UTC().Format(time.RFC3339Nano) }

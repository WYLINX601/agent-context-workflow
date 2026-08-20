package state

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
)

const schemaVersion = 2

type Session struct {
	MHGSessionID    string
	HermesStoredID  string
	GatewayIdentity string
	Profile         string
	CWD             string
	CreatedAt       string
	UpdatedAt       string
}

type Store struct {
	db   *sql.DB
	lock *flock.Flock
	mu   sync.Mutex
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("state path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	lock := flock.New(path + ".lock")
	locked, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire state database lock: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("state database is already in use: %s", path)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		_ = lock.Unlock()
		return nil, fmt.Errorf("open state database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, lock: lock}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		_ = lock.Unlock()
		return nil, err
	}
	return store, nil
}

func (s *Store) migrate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS bridge_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  mhg_session_id TEXT PRIMARY KEY,
  hermes_stored_id TEXT NOT NULL,
  gateway_identity TEXT NOT NULL,
  profile TEXT NOT NULL DEFAULT '',
  cwd TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT OR IGNORE INTO bridge_meta(key, value) VALUES ('schema_version', '2');
`)
	if err != nil {
		return fmt.Errorf("initialize state database: %w", err)
	}
	hasProfile, err := s.hasColumn("sessions", "profile")
	if err != nil {
		return fmt.Errorf("inspect state database schema: %w", err)
	}
	if !hasProfile {
		if _, err := s.db.Exec(`ALTER TABLE sessions ADD COLUMN profile TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migrate state database profile column: %w", err)
		}
	}
	if _, err := s.db.Exec(`UPDATE bridge_meta SET value = '2' WHERE key = 'schema_version'`); err != nil {
		return fmt.Errorf("update state database schema version: %w", err)
	}
	return nil
}

func (s *Store) hasColumn(table, wanted string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == wanted {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var dbErr error
	if s.db != nil {
		dbErr = s.db.Close()
	}
	if s.lock != nil {
		if err := s.lock.Unlock(); dbErr == nil {
			dbErr = err
		}
	}
	return dbErr
}

func (s *Store) Create(session Session) error {
	if session.MHGSessionID == "" || session.HermesStoredID == "" || session.GatewayIdentity == "" || session.CWD == "" {
		return errors.New("session mapping has missing required fields")
	}
	if session.CreatedAt == "" {
		session.CreatedAt = now()
	}
	if session.UpdatedAt == "" {
		session.UpdatedAt = session.CreatedAt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO sessions
 (mhg_session_id, hermes_stored_id, gateway_identity, profile, cwd, created_at, updated_at)
 VALUES (?, ?, ?, ?, ?, ?, ?)`, session.MHGSessionID, session.HermesStoredID, session.GatewayIdentity, session.Profile, session.CWD, session.CreatedAt, session.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save session mapping: %w", err)
	}
	return nil
}

func (s *Store) Get(mhgID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var session Session
	err := s.db.QueryRow(`SELECT mhg_session_id, hermes_stored_id, gateway_identity, profile, cwd, created_at, updated_at
 FROM sessions WHERE mhg_session_id = ?`, mhgID).Scan(
		&session.MHGSessionID, &session.HermesStoredID, &session.GatewayIdentity,
		&session.Profile, &session.CWD, &session.CreatedAt, &session.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, fmt.Errorf("session mapping not found: %s", mhgID)
	}
	if err != nil {
		return Session{}, fmt.Errorf("read session mapping: %w", err)
	}
	return session, nil
}

func (s *Store) UpdateLive(mhgID, storedID, cwd string) error {
	if storedID == "" || cwd == "" {
		return errors.New("stored id and cwd are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE sessions SET hermes_stored_id = ?, cwd = ?, updated_at = ? WHERE mhg_session_id = ?`, storedID, cwd, now(), mhgID)
	if err != nil {
		return fmt.Errorf("update session mapping: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("session mapping not found: %s", mhgID)
	}
	return nil
}

func (s *Store) UpdateCWD(mhgID, cwd string) error {
	if cwd == "" {
		return errors.New("cwd is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE sessions SET cwd = ?, updated_at = ? WHERE mhg_session_id = ?`, cwd, now(), mhgID)
	if err != nil {
		return fmt.Errorf("update session cwd: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("session mapping not found: %s", mhgID)
	}
	return nil
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

const SchemaVersion = schemaVersion

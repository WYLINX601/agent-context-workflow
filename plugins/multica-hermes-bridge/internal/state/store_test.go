package state

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSessionMappingDoesNotContainPromptFields(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Create(Session{
		MHGSessionID:    "mhg_test",
		HermesStoredID:  "stored-1",
		GatewayIdentity: "ws://127.0.0.1:9119/api/ws",
		Profile:         "product-solution",
		CWD:             "/tmp/project",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("mhg_test")
	if err != nil {
		t.Fatal(err)
	}
	if got.HermesStoredID != "stored-1" || got.Profile != "product-solution" || got.CWD != "/tmp/project" {
		t.Fatalf("unexpected mapping: %+v", got)
	}
	if err := store.UpdateLive("mhg_test", "stored-2", "/tmp/other"); err != nil {
		t.Fatal(err)
	}
	got, err = store.Get("mhg_test")
	if err != nil || got.HermesStoredID != "stored-2" || got.CWD != "/tmp/other" {
		t.Fatalf("updated mapping not persisted: %+v %v", got, err)
	}
}

func TestOpenMigratesLegacyStateDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE bridge_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO bridge_meta(key, value) VALUES ('schema_version', '1');
CREATE TABLE sessions (
  mhg_session_id TEXT PRIMARY KEY,
  hermes_stored_id TEXT NOT NULL,
  gateway_identity TEXT NOT NULL,
  cwd TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO sessions(mhg_session_id, hermes_stored_id, gateway_identity, cwd, created_at, updated_at)
VALUES ('mhg_legacy', 'stored-legacy', 'ws://127.0.0.1:9119/api/ws', '/tmp/project', 'created', 'updated');
`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	legacy, err := store.Get("mhg_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Profile != "" {
		t.Fatalf("legacy mapping should retain the default profile, got %q", legacy.Profile)
	}
}

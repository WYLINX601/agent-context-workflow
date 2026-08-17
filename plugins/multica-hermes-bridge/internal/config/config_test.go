package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndRejectRemote(t *testing.T) {
	t.Setenv("MHG_GATEWAY_URL", "")
	t.Setenv("MHG_PROFILE", "")
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if !cfg.UsedDefaults || cfg.Gateway.URL != "ws://127.0.0.1:9119/api/ws" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	path := filepath.Join(t.TempDir(), "remote.yaml")
	if err := os.WriteFile(path, []byte("gateway:\n  url: ws://192.0.2.10:9119/api/ws\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected remote endpoint to be rejected")
	}
}

func TestProfilePrecedenceAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("session:\n  profile: from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MHG_PROFILE", "from-env")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile() != "from-env" {
		t.Fatalf("environment profile did not override YAML: %q", cfg.Profile())
	}

	t.Setenv("MHG_PROFILE", "")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile() != "from-file" {
		t.Fatalf("YAML profile was not loaded: %q", cfg.Profile())
	}

	t.Setenv("MHG_PROFILE", "bad/profile")
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid profile to be rejected")
	}
}

func TestProfileIsPartOfGatewayLockIdentity(t *testing.T) {
	base := Defaults()
	named := base
	named.Session.Profile = "product-solution"
	if base.GatewayLockKey() == named.GatewayLockKey() {
		t.Fatal("profile must isolate gateway lock identity")
	}
}

func TestEnvironmentOverridesAreNotShownAsSecrets(t *testing.T) {
	t.Setenv("MHG_PROFILE", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("gateway:\n  token: should-not-print\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := cfg.SafeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || contains(string(data), "should-not-print") {
		t.Fatalf("secret leaked in config show: %s", data)
	}
}

func contains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

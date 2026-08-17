package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigCLIProfileOverridesEnvironment(t *testing.T) {
	t.Setenv("MHG_PROFILE", "from-env")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("session:\n  profile: from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, "from-cli")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Profile(); got != "from-cli" {
		t.Fatalf("CLI profile did not win precedence: %q", got)
	}
}

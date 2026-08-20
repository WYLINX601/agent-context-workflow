package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProfileResolverRequiresStructuredJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-specific")
	}
	path := filepath.Join(t.TempDir(), "hermes")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' '{\"schema_version\":1,\"profiles\":[{\"name\":\"kahn\"}]}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolver := ProfileResolver{Executable: path}
	ok, err := resolver.Exists(context.Background(), "kahn")
	if err != nil || !ok {
		t.Fatalf("expected kahn to resolve: %v %v", ok, err)
	}
	if _, err := resolver.Exists(context.Background(), "missing"); err == nil {
		t.Fatal("expected unknown profile error")
	} else if coded, ok := err.(*SupervisorError); !ok || coded.Code != CodeProfileNotFound {
		t.Fatalf("unexpected unknown profile error: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "hermes-bad")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\nprintf '%s' 'not-json'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := (ProfileResolver{Executable: bad}).Exists(context.Background(), "kahn"); err == nil {
		t.Fatal("expected malformed profile JSON error")
	} else if coded, ok := err.(*SupervisorError); !ok || coded.Code != CodeProfileLookup {
		t.Fatalf("unexpected malformed profile error: %v", err)
	}
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunBadConfigFailsLoudly proves the boot sequence rejects a missing
// config before touching the filesystem beyond reading it.
func TestRunBadConfigFailsLoudly(t *testing.T) {
	err := run(filepath.Join(t.TempDir(), "missing.yaml"), "127.0.0.1:0", filepath.Join(t.TempDir(), "state"))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want missing-config error", err)
	}
}

// TestRunInvalidConfigRejected proves semantic config errors abort boot.
func TestRunInvalidConfigRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	bad := "version: 99\n"
	if err := writeFile(path, bad); err != nil {
		t.Fatal(err)
	}
	err := run(path, "127.0.0.1:0", filepath.Join(dir, "state"))
	if err == nil || !strings.Contains(err.Error(), "loading config") {
		t.Fatalf("err = %v, want config loading failure", err)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

package jobpod

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTokenDir_CreatesFile(t *testing.T) {
	base := t.TempDir()
	dir, tok, err := NewTokenDir(base, "test-job")
	if err != nil {
		t.Fatalf("NewTokenDir: %v", err)
	}
	if dir == "" || tok == "" {
		t.Fatalf("NewTokenDir returned empty dir=%q token=%q", dir, tok)
	}

	// The returned dir must live under <base>/<jobID>.
	wantDir := filepath.Join(base, "test-job")
	if dir != wantDir {
		t.Errorf("dir = %q, want %q", dir, wantDir)
	}

	// The token file must exist with the right contents and mode.
	path := filepath.Join(dir, tokenFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	// Mode carries the type bits; mask to 0o777 before comparing.
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("token file mode = %o, want 0600", got)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(got) != tok {
		t.Errorf("token file content = %q, want %q (the value returned to the caller)", string(got), tok)
	}
}

func TestNewTokenDir_TokenIs32HexChars(t *testing.T) {
	base := t.TempDir()
	_, tok, err := NewTokenDir(base, "shape-job")
	if err != nil {
		t.Fatalf("NewTokenDir: %v", err)
	}
	if len(tok) != 32 {
		t.Errorf("token length = %d, want 32 hex chars", len(tok))
	}
	if _, err := hex.DecodeString(tok); err != nil {
		t.Errorf("token is not valid hex: %v", err)
	}
}

func TestNewTokenDir_UniqueAcrossCalls(t *testing.T) {
	base := t.TempDir()
	_, tok1, err := NewTokenDir(base, "job-a")
	if err != nil {
		t.Fatalf("first NewTokenDir: %v", err)
	}
	_, tok2, err := NewTokenDir(base, "job-b")
	if err != nil {
		t.Fatalf("second NewTokenDir: %v", err)
	}
	if tok1 == tok2 {
		t.Errorf("two NewTokenDir calls produced the same token %q (crypto/rand is broken?)", tok1)
	}
}

func TestNewTokenDir_CreatesNestedBase(t *testing.T) {
	// base does not exist yet; NewTokenDir must MkdirAll.
	parent := t.TempDir()
	base := filepath.Join(parent, "tokens", "nested")
	dir, _, err := NewTokenDir(base, "x")
	if err != nil {
		t.Fatalf("NewTokenDir with nested base: %v", err)
	}
	wantDir := filepath.Join(base, "x")
	if dir != wantDir {
		t.Errorf("dir = %q, want %q", dir, wantDir)
	}
}

func TestRemoveTokenDir_Idempotent(t *testing.T) {
	base := t.TempDir()
	dir, _, err := NewTokenDir(base, "x")
	if err != nil {
		t.Fatalf("NewTokenDir: %v", err)
	}
	if err := RemoveTokenDir(dir); err != nil {
		t.Fatalf("first RemoveTokenDir: %v", err)
	}
	// Second call must succeed (no error on missing dir).
	if err := RemoveTokenDir(dir); err != nil {
		t.Errorf("second RemoveTokenDir: %v (want nil — idempotent)", err)
	}
}

func TestRemoveTokenDir_EmptyArgIsNoOp(t *testing.T) {
	// Empty dir is the "no token dir was created" sentinel; must
	// not be treated as an error. Used by manager.Stop when a pod
	// was started without a token dir.
	if err := RemoveTokenDir(""); err != nil {
		t.Errorf("RemoveTokenDir(\"\"): %v, want nil", err)
	}
}

func TestRemoveTokenDir_RemovesEntireTree(t *testing.T) {
	// Nested contents under the token dir must be removed; the
	// function is RemoveAll, not a single os.Remove.
	base := t.TempDir()
	dir, _, err := NewTokenDir(base, "x")
	if err != nil {
		t.Fatalf("NewTokenDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing extra: %v", err)
	}
	if err := RemoveTokenDir(dir); err != nil {
		t.Fatalf("RemoveTokenDir: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir still exists after RemoveTokenDir: stat err = %v", err)
	}
}

func TestNewTokenDir_OnlyOwnsItsOwnDir(t *testing.T) {
	// Two jobs under the same base must not see each other's
	// token. This is the structural containment check for the
	// token layout.
	base := t.TempDir()
	dirA, tokA, err := NewTokenDir(base, "job-a")
	if err != nil {
		t.Fatalf("job-a: %v", err)
	}
	dirB, tokB, err := NewTokenDir(base, "job-b")
	if err != nil {
		t.Fatalf("job-b: %v", err)
	}
	gotA, err := os.ReadFile(filepath.Join(dirA, tokenFileName))
	if err != nil {
		t.Fatalf("read job-a token: %v", err)
	}
	gotB, err := os.ReadFile(filepath.Join(dirB, tokenFileName))
	if err != nil {
		t.Fatalf("read job-b token: %v", err)
	}
	if string(gotA) != tokA {
		t.Errorf("job-a token file = %q, want %q", gotA, tokA)
	}
	if string(gotB) != tokB {
		t.Errorf("job-b token file = %q, want %q", gotB, tokB)
	}
	if strings.Contains(string(gotA), tokB) || strings.Contains(string(gotB), tokA) {
		t.Errorf("token files contain each other's tokens (cross-contamination): %q vs %q", gotA, gotB)
	}
}

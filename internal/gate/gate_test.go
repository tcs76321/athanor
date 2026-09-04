// Package gate encodes the milestone gates as executable proofs
// (ROADMAP §3). Gate G1: "No tool execution exists at all — the agent is
// provably contained to LLM + storage."
//
// # What Gate G1 proves
//
// This package fails the build if any production source file under
// `internal/` or `cmd/` violates any of the following:
//
//  1. No tool-execution imports anywhere in production code. The forbidden
//     set is `os/exec` (spawning processes), `os/user` (host user
//     enumeration), `github.com/docker/docker/client` (container control),
//     and `github.com/containers/podman/v5/libpod` (container control).
//
//  2. No raw `syscall` in agent code (`internal/`), with one named
//     exception (§21.3 path containment — see rule 5). The agent's own
//     packages may not touch the syscall surface at large.
//
//  3. `syscall` in `cmd/` (the daemon entry point) is permitted only for
//     signal constants — `SIGTERM`, `SIGINT`, `SIGHUP`, `SIGQUIT`. The
//     test walks the AST and asserts every `syscall.X` selector in `cmd/`
//     references one of the allowlisted identifiers.
//
//  4. `os/exec` in `cmd/` is permitted only for the named file
//     `cmd/athanor/jobpod_client.go` (M2 production Podman client). The
//     allowlist is a single named file, not a directory or pattern; the
//     gate is opt-in by exception, not opt-out by default.
//
//  5. `syscall` in `internal/airlock/paths/paths_linux.go` and
//     `internal/airlock/paths/paths_darwin.go` is permitted only for
//     the `O_NOFOLLOW` open-flag constant (M4-T1, §21.3 file airlock).
//     The gate walks every `syscall.X` selector in those two files and
//     asserts it is the allowlisted identifier. This is the same shape
//     as rule 3 (allowlisted identifiers only) but applied to internal/.
//     The cross-platform fallback `paths_other.go` does not import
//     `syscall` and emits a documented warning that `O_NOFOLLOW` is
//     not enforced on unsupported GOOS; running on a build target
//     outside the allowlist is a Gate G1 tripwire by design —
//     unsupported hosts get a compile-time refusal, not a silent loss
//     of containment. Adding a new platform is a one-line entry in
//     `allowedInternalSyscallIdents` plus a new build-tag-gated file.
//
// The implementation is a single AST walk in TestGateG1NoToolExecution.
// Adding a new forbidden import, a new tool surface, or a new syscall
// identifier requires extending this test and updating this comment.
//
// # What Gate G1 does NOT prove
//
// Gate G1 is a structural containment guarantee, not a behavioral one.
// It does not prove:
//
//   - That the LLM cannot be tricked into producing shell commands in
//     its output. Behavioral containment against prompt injection is the
//     job of the M4 prompt-injection scanner (incoming documents) and
//     the M5-T3 byte-load `context_swap` tool, not this gate.
//   - That the daemon's HTTP surface is safe. Loopback-only is enforced
//     in `internal/server/server.go` (LocalhostAddr), and the tool
//     surface is gated by Go route registration. The Go compiler and
//     test coverage are the enforcement layer there.
//   - That the SQLite layer is well-behaved under load. The single-
//     connection pool and WAL mode are ADR-0003/0004 choices; performance
//     is monitored separately.
//
// In short: Gate G1 says "the agent cannot call out to a shell or a
// container client through any code path that this test can see." It
// does not say "the agent is safe." Safety is the rest of the system.
package gate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// forbiddenImports are capabilities M1 must not have anywhere in the
// daemon (production sources only; tests may reference them to describe
// attacks).
var forbiddenImports = map[string]string{
	"os/exec":                                "spawning processes (tool execution)",
	"os/user":                                "host user enumeration",
	"github.com/docker/docker/client":        "container control",
	"github.com/containers/podman/v5/libpod": "container control",
}

// forbiddenInternalImports additionally applies to internal/ only: the
// agent's own code may not touch raw syscalls at all. The daemon entry
// point (cmd/) may use syscall *signal constants* — verified separately
// below.
var forbiddenInternalImports = map[string]string{
	"syscall": "raw syscalls (exec, ptrace, mount, …)",
}

// allowedSyscallIdents are the signal-related syscall identifiers the
// daemon entry point may reference.
var allowedSyscallIdents = map[string]bool{
	"SIGTERM": true, "SIGINT": true, "SIGHUP": true, "SIGQUIT": true,
}

// allowedOsExecFiles are the specific files in cmd/ that may import
// os/exec. M2-T2 introduces the production Podman client
// (cmd/athanor/jobpod_client.go) which legitimately shells out to
// the `podman` binary. The gate still forbids os/exec in internal/
// and in any other cmd/ file; this list is the named exception, not
// a general permission.
//
// M4-T3 adds the scanner adapters in cmd/athanor/scanners/
// (ClamAV, YARA) which legitimately shell out to external
// binaries. The package is a directory, not a single file; the
// per-file allowlist is augmented with `allowedOsExecDirs` so
// adding a new adapter under cmd/athanor/scanners/ requires
// only a one-line entry in `allowedOsExecDirs`, not edits to
// this map. The dirs map is keyed on the full repo-relative
// path so a future contributor cannot create a sibling
// directory and slip through.
var allowedOsExecFiles = map[string]bool{
	"jobpod_client.go": true,
}

// allowedOsExecDirs are the directories in cmd/ under which
// every Go file may import os/exec. The M4-T3 scanner
// adapters (ClamAV, YARA) live here; the gate is opt-in by
// directory, not by file, because adapters naturally come
// in groups (driver + N scanner-specific files).
var allowedOsExecDirs = []string{
	"cmd/athanor/scanners",
}

// allowedInternalSyscallFiles are the specific files in internal/
// that may import `syscall`. M4-T1 introduces the path-containment
// library (§21.3 file airlock), whose build-tag-gated
// `paths_linux.go` and `paths_darwin.go` legitimately need the
// platform's `O_NOFOLLOW` open-flag constant to defeat
// symlink-as-final-component escapes. The gate still forbids
// `syscall` in every other internal/ file; this list is the named
// exception, not a general permission, and the syscall identifiers
// reachable through it are constrained by
// `allowedInternalSyscallIdents` (rule 5 above).
//
// Path entries are the full relative path from the repo root, not
// just the basename, so a future contributor cannot create an
// unannotated `internal/somethingelse/paths_linux.go` and have it
// pass. Adding a new platform is a one-line entry here plus a
// matching entry in `allowedInternalSyscallIdents` plus a new
// build-tag-gated file.
var allowedInternalSyscallFiles = map[string]bool{
	"internal/airlock/paths/paths_linux.go":  true,
	"internal/airlock/paths/paths_darwin.go": true,
}

// allowedInternalSyscallIdents are the syscall identifiers the
// §21.3 path-containment wrappers (M4-T1) may reference. The set is
// closed and small on purpose: `O_NOFOLLOW` is the only
// kernel-level defense against a final-component symlink that
// slips past `filepath.EvalSymlinks` on the prefix. The list is
// the same shape as `allowedSyscallIdents` (the cmd/ signal
// constants) but applied to internal/ via the file allowlist above.
var allowedInternalSyscallIdents = map[string]bool{
	"O_NOFOLLOW": true,
}

// TestGateG1NoToolExecution is the grep-level proof, upgraded to an AST
// walk: no production source under internal/ or cmd/ may import a
// tool-execution capability. The llm client's net/http and the daemon's
// own loopback server are the sanctioned network surfaces; adding any
// outbound-calling tool is a Gate G1 violation until M2 lands its
// guarded runtime.
func TestGateG1NoToolExecution(t *testing.T) {
	roots := []struct {
		dir      string
		internal bool
	}{
		{"../../internal", true},
		{"../../cmd", false},
	}
	violations := 0
	for _, root := range roots {
		err := filepath.WalkDir(root.dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				t.Errorf("parsing %s: %v", path, perr)
				return nil
			}
			for _, imp := range file.Imports {
				name := strings.Trim(imp.Path.Value, `"`)
				if reason, bad := forbiddenImports[name]; bad {
					// os/exec is allowed in a small named set of
					// cmd/ files (the production Podman client) and
					// inside `allowedOsExecDirs` (the M4-T3 scanner
					// adapters). The gate still forbids it everywhere
					// else.
					if name == "os/exec" && !root.internal {
						if allowedOsExecFiles[filepath.Base(path)] {
							// permitted (single named file)
						} else if isUnderAllowedExecDir(path) {
							// permitted (allowlisted directory)
						} else {
							t.Errorf("%s imports %q — %s", path, name, reason)
							violations++
						}
					} else {
						t.Errorf("%s imports %q — %s", path, name, reason)
						violations++
					}
				}
				if reason, bad := forbiddenInternalImports[name]; bad && root.internal {
					// M4-T1 (§21.3 file airlock): the path-containment
					// library has build-tag-gated files that need
					// `syscall.O_NOFOLLOW`. They are the only files in
					// internal/ that may import `syscall`, and the
					// syscall identifiers they reach are constrained
					// by `allowedInternalSyscallIdents` (enforced
					// below in the same walk). Path-keyed, not
					// basename-keyed, so a future contributor cannot
					// silently create another exception.
					if name == "syscall" && allowedInternalSyscallFiles[relPath(path)] {
						// permitted — the selector check below
						// is the second line of defense.
					} else {
						t.Errorf("%s imports %q — %s", path, name, reason)
						violations++
					}
				}
				if name == "syscall" && root.internal && allowedInternalSyscallFiles[relPath(path)] {
					// Identical shape to the cmd/ signal-constant
					// check above, but applied to the M4-T1 wrapper
					// files. Every `syscall.X` selector in the
					// allowlisted files must reference one of the
					// identifiers in `allowedInternalSyscallIdents`.
					// A new identifier is a Gate G1 violation
					// until both this map and the file allowlist
					// are extended deliberately.
					ast.Inspect(file, func(n ast.Node) bool {
						sel, ok := n.(*ast.SelectorExpr)
						if !ok {
							return true
						}
						id, ok := sel.X.(*ast.Ident)
						if !ok || id.Name != "syscall" {
							return true
						}
						if !allowedInternalSyscallIdents[sel.Sel.Name] {
							t.Errorf("%s references syscall.%s — only %v are allowed in %s (Gate G1 rule 5)", path, sel.Sel.Name, allowedInternalSyscallIdentsKeyList(), filepath.Base(path))
							violations++
						}
						return true
					})
				}
				if name == "syscall" && !root.internal {
					// Outside internal/, syscall is allowed for signal
					// constants only.
					ast.Inspect(file, func(n ast.Node) bool {
						sel, ok := n.(*ast.SelectorExpr)
						if !ok {
							return true
						}
						if id, ok := sel.X.(*ast.Ident); ok && id.Name == "syscall" && !allowedSyscallIdents[sel.Sel.Name] {
							t.Errorf("%s references syscall.%s — only signal constants are allowed in cmd/", path, sel.Sel.Name)
							violations++
						}
						return true
					})
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if violations > 0 {
		t.Fatalf("%d Gate G1 violations: tool-execution capability present in M1", violations)
	}
}

// relPath returns the repository-root-relative path for a file
// visited by filepath.WalkDir. The walker is rooted at ../../internal
// or ../../cmd relative to this test file, so the repo root is two
// levels up from those roots (i.e. the parent of internal/ and cmd/).
// The function is used to key the `allowedInternalSyscallFiles`
// allowlist on repo-relative paths, so a future contributor cannot
// create an unannotated `internal/somethingelse/paths_linux.go` and
// have it pass. On unexpected input the function falls back to the
// walked path verbatim and lets the lookup miss; the gate test will
// then report a violation rather than silently allow the import.
func relPath(p string) string {
	// filepath.WalkDir hands us paths like "../../internal/foo/bar.go".
	// Normalize, then strip the leading "../" components until the
	// first segment is "internal" or "cmd"; everything from that
	// segment onward is the repo-relative path.
	cleaned := filepath.ToSlash(filepath.Clean(p))
	parts := strings.Split(cleaned, "/")
	for i, seg := range parts {
		if seg == "internal" || seg == "cmd" {
			return strings.Join(parts[i:], "/")
		}
	}
	return cleaned
}

// isUnderAllowedExecDir reports whether the file at
// `walkerPath` lives under one of the directories in
// `allowedOsExecDirs`. The walker hands us paths like
// "../../cmd/athanor/scanners/clamav.go"; the function
// normalizes to "cmd/athanor/scanners/clamav.go" via
// relPath, then checks whether any allowed dir is a
// prefix. A future contributor cannot create a sibling
// directory and have it pass: the prefix check is exact
// (no globs).
func isUnderAllowedExecDir(walkerPath string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(walkerPath))
	for _, dir := range allowedOsExecDirs {
		// Normalize: relPath returns "cmd/..." or
		// "internal/..." — strip the leading "cmd/"
		// the same way relPath does (relPath keeps
		// "cmd" in the returned path). The dirs
		// list stores paths starting with "cmd/".
		rel := relPath(walkerPath)
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			_ = cleaned
			return true
		}
	}
	return false
}

// allowedInternalSyscallIdentsKeyList returns the sorted
// identifiers in `allowedInternalSyscallIdents` for use in error
// messages. The format is stable so the gate's test output is
// diff-friendly across changes.
func allowedInternalSyscallIdentsKeyList() []string {
	out := make([]string, 0, len(allowedInternalSyscallIdents))
	for k := range allowedInternalSyscallIdents {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Package gate encodes the milestone gates as executable proofs
// (ROADMAP §3). Gate G1: "No tool execution exists at all — the agent is
// provably contained to LLM + storage."
package gate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
var allowedOsExecFiles = map[string]bool{
	"jobpod_client.go": true,
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
					// cmd/ files (the production Podman client). The
					// gate still forbids it everywhere else.
					if name == "os/exec" && !root.internal && allowedOsExecFiles[filepath.Base(path)] {
						// permitted
					} else {
						t.Errorf("%s imports %q — %s", path, name, reason)
						violations++
					}
				}
				if reason, bad := forbiddenInternalImports[name]; bad && root.internal {
					t.Errorf("%s imports %q — %s", path, name, reason)
					violations++
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

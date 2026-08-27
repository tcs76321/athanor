// Package deps enforces the project's two-dependency policy as an
// executable test. AGENTS.md states: "The project is deliberately lean:
// two deps today (`mattn/go-sqlite3`, `gopkg.in/yaml.v3`). Adding a
// dependency is a project decision, not an agent decision."
//
// This test reads `go.mod` from the repo root and fails the build if
// the number of `require` lines (the direct-dependency set) exceeds
// the documented cap. The cap is held to 2 to make the policy
// structural rather than aspirational: a third dep cannot land
// without flipping this test and updating the AGENTS.md agreement.
//
// What this test does NOT cover:
//
//   - Transitive dependencies. These are out of our control without
//     dropping the direct dep that pulls them in; if a future dep
//     drags in something unacceptable, the response is to find an
//     alternative direct dep, not to bump this cap.
//   - Test-only deps. The Go module system has no `require test`
//     directive as of Go 1.17+; everything in `require` is treated
//     uniformly. The intended workflow if a test-only dep is needed
//     is to ship it as `//go:build` gated code that the production
//     binary doesn't compile, then add the require with explicit
//     justification in the PR and the AGENTS.md update.
//
// The test is in `internal/deps/` rather than `internal/gate/` because
// it's a different kind of guarantee: structural on the module file,
// not on the source AST. Gate G1 stays focused on tool-execution
// containment; this stays focused on the dependency policy.
package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// maxDirectDeps is the documented cap. AGENTS.md names the two current
// deps: `mattn/go-sqlite3` and `gopkg.in/yaml.v3`.
const maxDirectDeps = 2

func TestDirectDependencyCap(t *testing.T) {
	// Walk up from this test's directory to the module root. The
	// test file lives at internal/deps/deps_test.go, so two `..`
	// hops reach the module root.
	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("finding module root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	got := countDirectRequires(string(raw))
	if got > maxDirectDeps {
		t.Errorf("go.mod has %d direct dependencies (cap is %d): %s\n"+
			"Adding a dependency is a project decision, not an agent decision. "+
			"See AGENTS.md and update both this test and the AGENTS.md note "+
			"if the cap is intentionally raised.",
			got, maxDirectDeps, listDirectRequires(string(raw)))
	}
}

// findModuleRoot walks up the directory tree looking for go.mod.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// countDirectRequires counts the `require <path> <version>` lines in
// the `require (...)` block at the bottom of go.mod (or as flat
// `require` lines, the older format). Indirect requires (the `//
// indirect` marker) are not counted; they are transitive and out of
// scope for this test.
//
// A require line matches the pattern: `require <path> <version>`
// where `<path>` contains at least one `/`. The first token is
// literally "require"; the second is the path; the third is the
// version. We don't try to fully parse the go.mod grammar — just
// the require lines, which is all the test needs.
func countDirectRequires(goMod string) int {
	count := 0
	for _, raw := range strings.Split(goMod, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		// Skip the `require (` opener, the closing `)`, the
		// `module` line, and the `go` directive.
		if line == "require (" || line == ")" ||
			strings.HasPrefix(line, "module ") ||
			strings.HasPrefix(line, "go ") {
			continue
		}
		// A require line starts with the literal word "require"
		// followed by a path that contains "/". A line like
		// `require gopkg.in/yaml.v3 v3.0.1` matches; a line like
		// `replace (...)` would not (and we don't need to handle
		// `replace` for this test).
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "require" {
			continue
		}
		if strings.Contains(fields[1], "/") {
			// Skip indirect — out of scope.
			if !strings.Contains(line, "// indirect") {
				count++
			}
		}
	}
	return count
}

// listDirectRequires returns a comma-separated list of direct require
// paths for error messages. Same parsing rules as countDirectRequires.
func listDirectRequires(goMod string) string {
	var paths []string
	for _, raw := range strings.Split(goMod, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if line == "require (" || line == ")" ||
			strings.HasPrefix(line, "module ") ||
			strings.HasPrefix(line, "go ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "require" {
			continue
		}
		if strings.Contains(fields[1], "/") && !strings.Contains(line, "// indirect") {
			paths = append(paths, fields[1])
		}
	}
	return strings.Join(paths, ", ")
}

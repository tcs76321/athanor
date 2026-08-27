// Gate G2 (M2-T3, ADR-0008): structural proof that the internal
// API at /internal/v1/ is correctly contained.
//
// Gate G1 forbids tool-execution in the agent's own code
// (os/exec, container clients, raw syscalls). Gate G2 adds the
// M2-T3-specific structural guarantees that come with exposing a
// pod-callable HTTP surface:
//
//  1. internal/internalapi/ has no dependency on internal/llm.
//     The internal API serves the pod, not the model; if a
//     future change imports the LLM client, the pod would
//     have an indirect path to model calls that bypasses the
//     tool surface — a Gate G2 violation.
//
//  2. internal/internalapi/middleware.go references
//     crypto/subtle.ConstantTimeCompare by name. The
//     constant-time token compare is the structural defense
//     against timing attacks; bypassing it with `==` is a
//     Gate G2 violation. The text-search is structural, not a
//     runtime test of constant-time behavior — see the
//     test for the rationale.
//
//  3. Every http.Handle call under internal/internalapi/ that
//     registers a /internal/v1/ path is reached through
//     authMiddleware. The structural check is "handlers.go
//     contains the literal authMiddleware(a.tokens, ...)" —
//     we don't parse the AST for this; a grep on the file is
//     enough because handlers.go is the only registration
//     point.
//
// What Gate G2 does NOT prove:
//
//   - That the bearer token never leaks. (Covered by
//     TestToken_NotInPodmanArgv in internal/jobpod and by
//     not logging the token in middleware.go.)
//   - That every future handler goes through the middleware.
//     The structural check requires that handlers.go
//     references authMiddleware; a new file that registers
//     handlers on a different mux would not be caught. The
//     intended discipline is "all internal API routes are
//     registered in handlers.go" — a package convention, not
//     a Go enforcement.
//   - That the auth flow is correct. The behavioral tests in
//     internal/internalapi/internalapi_test.go exercise
//     the happy and unhappy paths; Gate G2 is the structural
//     backstop only.
package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)
// internalapiDir is the package whose surface Gate G2
// constrains. Relative to the test file (internal/gate/).
const internalapiDir = "../../internal/internalapi"

// TestGateG2NoLLMDependency walks internal/internalapi/ and
// asserts no production source imports internal/llm. The
// internal API is the only path a Job Pod has to do anything;
// if it imported the LLM client, the pod would have an indirect
// model-call path that bypasses every other containment check.
func TestGateG2NoLLMDependency(t *testing.T) {
	violations := 0
	err := filepath.WalkDir(internalapiDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// Cheap text search: any line containing the import
		// path is a violation. We use text rather than the
		// AST because the import path string is unique
		// enough to be unambiguous and we want the gate
		// test to be readable in isolation.
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "github.com/tcs76321/athanor/internal/llm") {
				t.Errorf("%s imports internal/llm — internal API must not depend on the LLM client (Gate G2)", path)
				violations++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if violations > 0 {
		t.Fatalf("%d Gate G2 violations: internal API depends on internal/llm", violations)
	}
}

// TestGateG2ConstantTimeComparePresent is the structural
// guarantee that the bearer-token compare uses crypto/subtle.
//
// Bypassing ConstantTimeCompare with `==` would re-introduce a
// timing side channel; the structural check is a backstop, not
// the only defense. The actual constant-time property is exercised
// in unit tests (TestAuthMiddleware_AcceptsValidToken and friends)
// — those tests are behavioral, this gate is structural.
func TestGateG2ConstantTimeComparePresent(t *testing.T) {
	middleware := filepath.Join(internalapiDir, "middleware.go")
	raw, err := os.ReadFile(middleware)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ConstantTimeCompare") {
		t.Errorf("%s does not reference ConstantTimeCompare; the bearer-token compare must be constant-time (Gate G2)", middleware)
	}
}


// TestGateG2InternalAPIRoutesGoThroughMiddleware asserts every
// route registered under /internal/v1/ in handlers.go is reached
// through authMiddleware. The check is structural (text search):
// handlers.go must reference authMiddleware, and every
// /internal/v1/ route registration must be paired with it.
//
// We count `mux.Handle("..."` calls (one per route registration)
// and `authMiddleware(a.tokens,` calls (one per wrap). The two
// must match. We do not count occurrences of "/internal/v1/" in
// the file because a path pattern + a comment can both mention
// the prefix; counting Handle calls is unambiguous.
func TestGateG2InternalAPIRoutesGoThroughMiddleware(t *testing.T) {
	handlers := filepath.Join(internalapiDir, "handlers.go")
	raw, err := os.ReadFile(handlers)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	// The file must reference authMiddleware at all.
	if !strings.Contains(body, "authMiddleware(") {
		t.Errorf("%s does not reference authMiddleware; every /internal/v1/ route must be wrapped (Gate G2)", handlers)
		return
	}

	// Count every mux.Handle call (each line is one route
	// registration in this file's style).
	handleCount := strings.Count(body, "mux.Handle(")
	wrapCount := strings.Count(body, "authMiddleware(a.tokens,")
	if handleCount == 0 {
		t.Errorf("%s has no mux.Handle calls; the API registers no routes (Gate G2)", handlers)
		return
	}
	if wrapCount != handleCount {
		t.Errorf("%s: %d mux.Handle calls but %d authMiddleware wraps; every route must be wrapped (Gate G2)",
			handlers, handleCount, wrapCount)
	}
}


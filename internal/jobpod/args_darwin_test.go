//go:build darwin

package jobpod

import (
	"strings"
	"testing"
)

// TestBuildArgs_DarwinSeccompAbsent (M2-T2 acceptance, Darwin) asserts
// the seccomp flag is NOT in the argv on macOS, where it would be
// silently accepted by podman but enforced by nothing. ADR-0007
// documents the caveat. The honest test is the one that says "we are
// not pretending" rather than papering over with a comment.
func TestBuildArgs_DarwinSeccompAbsent(t *testing.T) {
	args := buildArgs(sampleSpec())
	joined := joinArgs(args)
	if strings.Contains(joined, "seccomp") {
		t.Errorf("darwin argv contains seccomp; podman would accept it but enforce nothing\ngot: %s", joined)
	}
}

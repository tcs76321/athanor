//go:build linux

package jobpod

import (
	"strings"
	"testing"
)

// TestBuildArgs_LinuxSeccompPresent (M2-T2 acceptance, Linux) asserts
// the seccomp flag is in the argv. seccomp is a meaningful kernel
// enforcement on Linux; its absence is a real containment regression.
func TestBuildArgs_LinuxSeccompPresent(t *testing.T) {
	args := buildArgs(sampleSpec())
	joined := joinArgs(args)
	if !strings.Contains(joined, "seccomp=runtime/default") {
		t.Errorf("linux argv missing seccomp profile\ngot: %s", joined)
	}
}

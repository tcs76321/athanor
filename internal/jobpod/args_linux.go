//go:build linux

package jobpod

// platformHardening returns the §21.2 hardening flag set for Linux.
// seccomp is meaningful here (the kernel enforces it); the runtime/
// default profile is podman's stock safe set and is the explicit
// choice for M2.
func platformHardening() []string {
	return []string{
		"--security-opt", "seccomp=runtime/default",
	}
}

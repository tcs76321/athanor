//go:build darwin

package jobpod

// platformHardening returns the §21.2 hardening flag set for Darwin
// (macOS / podman-machine / applehv). seccomp is a no-op on the
// applehv VM (no kernel seccomp available); ADR-0007 documents the
// caveat. M2-T6's security suite is where Linux seccomp enforcement
// is verified end-to-end.
func platformHardening() []string {
	return nil
}

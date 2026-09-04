//go:build !linux && !darwin

package paths

import "log"

// noFollowFlag returns 0 on unsupported platforms. The
// O_NOFOLLOW defense is not enforced; the package-init
// warning below is the audit trail. M4 is targeted at
// Linux + Darwin (per docs/demo-m2.md and the project
// host matrix); a new platform is a one-line entry in
// allowedInternalSyscallIdents in internal/gate plus a
// build-tag-gated file alongside this one.
func noFollowFlag() int {
	return 0
}

func init() {
	log.SetFlags(0)
	log.Println("paths: O_NOFOLLOW not enforced on this GOOS; add a build-tag-gated file to enable it.")
}

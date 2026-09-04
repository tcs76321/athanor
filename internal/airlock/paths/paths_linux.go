//go:build linux

package paths

// O_NOFOLLOW is the only syscall identifier the §21.3 path-
// containment library reaches. Gate G1's rule 5 allowlists
// this file; any new identifier here is a Gate G1 violation
// until `allowedInternalSyscallIdents` in
// internal/gate/gate_test.go is extended deliberately.
import "syscall"

// noFollowFlag returns O_NOFOLLOW on Linux. The kernel
// rejects open() with ELOOP if the final component is a
// symlink, closing the TOCTOU window between Validate's
// Lstat and the open(2) syscall.
func noFollowFlag() int {
	return syscall.O_NOFOLLOW
}

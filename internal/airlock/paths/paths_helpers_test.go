// Test-only probes for host-filesystem behavior. Some host
// filesystems (notably macOS user directories) silently drop the
// setuid or setgid bit on os.Chmod, even when the syscall returns
// success. TestAdversarialCorpus uses these probes to skip the
// affected rows on those hosts; the production code in paths.go
// is unchanged.
package paths

import (
	"os"
	"testing"
)

// modeReportsSetUID reports whether the host filesystem honored
// the setuid bit that os.Chmod was asked to set on path.
func modeReportsSetUID(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSetuid != 0
}

// modeReportsSetGID is the setgid sibling of modeReportsSetUID.
func modeReportsSetGID(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSetgid != 0
}

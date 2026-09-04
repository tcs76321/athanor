//go:build windows

package paths

// mkFifo is a no-op on Windows: FIFOs require Cygwin or a
// special ACL there. paths_test.go gates the device-FIFO row
// on runtime.GOOS so the row is only exercised on platforms
// where this helper actually creates a FIFO.
func mkFifo(path string) error {
	return nil
}

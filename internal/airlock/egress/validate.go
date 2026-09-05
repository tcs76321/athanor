package egress

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tcs76321/athanor/internal/airlock/paths"
)

// ValidateTree walks the directory tree under `root` and
// applies the path-containment library's Validate to
// every entry. A single rejection aborts the whole walk
// and returns the rejection as an error.
//
// The function is the structural defense against a
// poisoned export tree: even if every scanner passes on
// the artifact as a whole, a single setuid binary or
// device node in the export tree is enough to fail the
// export closed. The audit row carries the first failing
// path and the path error.
//
// # Why walk the whole tree?
//
// The egress pipeline writes artifacts that may be
// multi-file (a `code` artifact is a directory of source
// files; a `document` artifact is a single file). A
// path-layer check on the artifact root alone misses
// malicious content placed in subdirectories. The walk
// is a single-pass `filepath.WalkDir` with an early
// return on the first failure.
//
// # Originals-not-touched (re-stated)
//
// ValidateTree reads the directory tree; it does not
// open files (other than `os.Lstat` for the type/mode
// check). The artifact's bytes are unchanged on disk
// regardless of the validation result.
func ValidateTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relpath %s: %w", path, err)
		}
		if rel == "." {
			// The root itself: skip. The Validate call
			// on the root with rel="." returns the
			// root, which is a no-op for path checking.
			return nil
		}
		// WalkDir can hand us a path whose relpath
		// starts with ".." on certain root layouts
		// (e.g. a symlinked workspace). Reject
		// defensively.
		if strings.HasPrefix(rel, "..") {
			return fmt.Errorf("path %q escapes tree root", rel)
		}
		// The Validate call returns the resolved path
		// on success; the Lstat inside the library
		// catches symlink escapes, setuid, devices,
		// and unexpected executables.
		if _, err := paths.Validate(root, rel, paths.ValidateOptions{}); err != nil {
			return fmt.Errorf("validate %s: %w", rel, err)
		}
		return nil
	})
}

// ExportTree is the on-disk shape of an accepted
// artifact's export: a directory under
// `exports/<project>/<artifact>-<sha12>/` containing the
// artifact's bytes. The exporter copies the artifact
// from the artifact store into this directory in a
// single pass.
//
// The shape is fixed: every accepted artifact is a
// directory, never a single file. The `code` archetype
// is multi-file by construction; the `text` and
// `document` archetypes are single-file but live inside
// the directory as `<artifactID>.<kind>` for symmetry
// with multi-file artifacts. The artifact store's
// `ReadContent` returns the bytes; the directory layout
// is the egress package's concern.
type ExportTree struct {
	// Dir is the directory the exporter will write to.
	Dir string
}

// EnsureDir creates the export directory if it does
// not exist. Idempotent.
func (e *ExportTree) EnsureDir() error {
	return os.MkdirAll(e.Dir, 0o700)
}

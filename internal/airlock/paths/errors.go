package paths

import "errors"

// ErrAbsolute is returned when the candidate path is absolute
// (starts with "/") or, on Windows, has a drive letter. Absolute
// paths can never be contained to a root. The error wraps the
// offending input so the caller can surface it in audit events.
var ErrAbsolute = errors.New("paths: absolute path rejected")

// ErrTraversal is returned when the candidate path contains a
// ".." segment that escapes the root after Clean, or when the
// cleaned path is not lexically a descendant of root. The
// library never opens the traversal target.
var ErrTraversal = errors.New("paths: traversal segment rejected")

// ErrSymlinkEscape is returned when the candidate path resolves
// through a symlink whose target is outside the root. The
// library uses filepath.EvalSymlinks on the root-prefixed
// absolute path to detect this; the final-component symlink
// defense is O_NOFOLLOW (OpenNoFollow), enforced at the kernel.
var ErrSymlinkEscape = errors.New("paths: symlink escape rejected")

// ErrDevice is returned when the candidate path is a device
// node (block, char, or FIFO). Setuid/setgid detection has its
// own error (ErrSetUID) so callers can distinguish "do not
// execute" from "do not trust" — devices are never useful
// workspace files.
var ErrDevice = errors.New("paths: device node rejected")

// ErrSetUID is returned when the candidate path has the setuid
// or setgid bit set. The §21.3 egress rule rejects setuid
// binaries; a setuid file inside a workspace is either a
// misconfiguration or an attack and is never acceptable.
var ErrSetUID = errors.New("paths: setuid/setgid bit rejected")

// ErrExecutable is returned when the candidate path is an
// executable and the caller passed AllowExecutable=false to
// Validate. The §21.3 egress rule is "reject unexpected
// executables" — the policy is opt-out, the default is reject.
var ErrExecutable = errors.New("paths: executable rejected")

// ErrNotFound is returned when the candidate path does not
// exist. A separate error makes "missing" a recoverable case
// for ingress pipelines (the pipeline may create the parent
// directory) distinct from the structural-rejection errors
// above (which are never recoverable for the same input).
var ErrNotFound = errors.New("paths: file not found")

// ErrInvalid is returned when the candidate path contains a
// NULL byte or other byte sequence that the OS cannot accept
// in a path. This is a precondition failure, not a containment
// decision; the caller should log and move on.
var ErrInvalid = errors.New("paths: invalid path bytes")

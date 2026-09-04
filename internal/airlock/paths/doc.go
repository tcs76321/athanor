// Package paths is the §21.3 file-airlock path-containment
// library for the M4 milestone. Every file operation the agent
// performs (read, write, ingest, export) routes through this
// package before the kernel sees a syscall.
//
// The library enforces three classes of rejection, matching the
// ARCHITECTURE.md §21.3 rules:
//
//  1. Structural: absolute paths, traversal ("../"), NULL bytes,
//     and any path whose final component is a symlink that
//     escapes the root. The library never opens the target of
//     such a symlink; it refuses to compute the open syscall
//     in the first place.
//
//  2. File-mode: device nodes (block, char, FIFO), setuid/setgid
//     bits, and unexpected executables (when the caller opts
//     into the check).
//
//  3. Final-component symlink defense: even after the structural
//     checks pass, OpenNoFollow issues the open with O_NOFOLLOW
//     so the kernel rejects a symlink at the leaf. This is the
//     last line of defense against a TOCTOU window between
//     Lstat and open. The O_NOFOLLOW constant is reached
//     through a build-tag-gated file (paths_linux.go,
//     paths_darwin.go) that is the only place under internal/
//     that imports `syscall`; Gate G1's rule 5 allows it.
//
// The package is pure: no network, no process spawn, no
// container control. Gate G1 stays green.
package paths

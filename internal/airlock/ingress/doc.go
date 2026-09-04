// Package ingress is the §21.3 file-airlock ingress pipeline
// (ROADMAP M4-T2; ADR-0015). It observes files in
// `workspace/inbox/`, routes them through `airlock/paths` for
// path-layer validation and through `airlock/scanner` for
// content scanning, and either copies the bytes to
// `workspace/inbox/.processed/<sha256>.<ext>` (clean) or
// quarantines them under `workspace/quarantine/<date>/<sha256>.<ext>`.
//
// # Originals untouched
//
// The user's file at `inbox/<name>` is never `os.Remove`'d,
// `os.Rename`'d, or `os.Chmod`'d. The pipeline only *reads*
// the user's file and *writes* new files under
// `.processed/` or `quarantine/`. This is the strongest AC
// in the M4-T2 acceptance criterion and the only one that
// protects the user from losing data they deliberately placed
// in the inbox. A test (`TestIngress_OriginalsUntouched`)
// asserts the invariant end-to-end.
//
// # Idempotency
//
// Idempotency is content-hash based. Before scanning, the
// processor computes the SHA-256 of the file bytes. A second
// event for the same content finds the `quarantined_files`
// row by primary key, or finds the `.processed/<sha256>.*`
// marker, and drops the event with a `duplicate_ignored`
// audit row. This makes the pipeline safe to run
// concurrently and safe to re-trigger on retry.
//
// # Kill switch
//
// The Watcher honors a Freezer interface (§22.1): when the
// daemon is frozen, the processor stops draining the queue
// but does not lose events. On unfreeze, the queue drains
// normally. The Watcher is constructed with a `Freezer`
// callback so the package has no dependency on
// `internal/control`.
package ingress

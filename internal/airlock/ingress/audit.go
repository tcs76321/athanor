package ingress

import (
	"context"
	"encoding/json"

	"github.com/tcs76321/athanor/internal/airlock/scanner"
	"github.com/tcs76321/athanor/internal/store"
)

// jsonMarshal is a thin alias so scannerResultDetailsJSON
// stays short. Encoding/json is imported in this file (audit
// helpers) and reused by ingress.go.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// auditAccepted writes the `airlock` audit row for a clean
// disposition. The row records the SHA-256, the per-scanner
// breakdown, and the source byte count. The RelPath is
// preserved verbatim so the operator can correlate the
// audit row with the file in their inbox.
func (w *Watcher) auditAccepted(ctx context.Context, pf pendingFile, shaHex string, size int64, result scanner.PipelineResult) {
	data := auditPayload{
		Event:      string(EventAccepted),
		SHA256:     shaHex,
		RelPath:    pf.relPath,
		SourceSize: size,
		Pipeline:   "ingress",
		Scanners:   result.Details,
	}
	w.writeAudit(ctx, data)
}

// auditQuarantined writes the `airlock` audit row for an
// uncertain or rejected disposition. The row includes the
// full per-scanner breakdown; the quarantine row in the DB
// (written by QuarantineRepo.Put) carries a parallel
// audit row keyed on the same SHA-256.
func (w *Watcher) auditQuarantined(ctx context.Context, pf pendingFile, shaHex string, size int64, storedPath string, result scanner.PipelineResult) {
	data := auditPayload{
		Event:      string(EventQuarantined),
		SHA256:     shaHex,
		RelPath:    pf.relPath,
		Reason:     result.Reason,
		StoredPath: storedPath,
		SourceSize: size,
		Pipeline:   "ingress",
		Scanners:   result.Details,
	}
	w.writeAudit(ctx, data)
}

// auditRejected writes the `airlock` audit row for a
// path-layer rejection. The file stays in inbox; this row
// is the operator's only signal that the pipeline saw the
// file and refused to act on it.
func (w *Watcher) auditRejected(ctx context.Context, pf pendingFile, reason string) {
	data := auditPayload{
		Event:    string(EventRejected),
		RelPath:  pf.relPath,
		Reason:   reason,
		Pipeline: "ingress",
	}
	w.writeAudit(ctx, data)
}

// auditDuplicate writes the `airlock` audit row for a
// duplicate-content event. The location string is
// "quarantine" (already in the quarantine table) or
// "processed" (already in .processed/).
func (w *Watcher) auditDuplicate(ctx context.Context, pf pendingFile, shaHex, location string) {
	data := auditPayload{
		Event:    string(EventDuplicateIgnored),
		SHA256:   shaHex,
		RelPath:  pf.relPath,
		Reason:   "duplicate:already-" + location,
		Pipeline: "ingress",
	}
	w.writeAudit(ctx, data)
}

// auditPayload is the on-the-wire shape of the `data` field
// in the events table. The top-level event row also carries
// a synthetic `event` key (the row's primary dispatcher)
// so a SQL filter on events.data_json can find rows without
// parsing JSON.
type auditPayload struct {
	Event      string         `json:"event"`
	SHA256     string         `json:"sha256,omitempty"`
	RelPath    string         `json:"relpath"`
	Reason     string         `json:"reason,omitempty"`
	StoredPath string         `json:"stored_path,omitempty"`
	SourceSize int64          `json:"source_size,omitempty"`
	Pipeline   string         `json:"pipeline,omitempty"`
	Scanners   map[string]any `json:"scanners,omitempty"`
}

// writeAudit persists one event row. Errors are logged but
// not fatal; the audit row is best-effort and the file
// disposition is the durable record (the .processed/
// marker or the quarantine row).
func (w *Watcher) writeAudit(ctx context.Context, data auditPayload) {
	raw, err := json.Marshal(data)
	if err != nil {
		w.logger.Warn("ingress: audit marshal failed", "event", data.Event, "err", err)
		return
	}
	if _, err := w.store.AppendEvent(ctx, store.Event{
		Category: "airlock",
		Level:    store.EventInfo,
		Data:     map[string]any{"event": data.Event, "data": json.RawMessage(raw)},
	}); err != nil {
		w.logger.Warn("ingress: audit append failed", "event", data.Event, "err", err)
	}
}

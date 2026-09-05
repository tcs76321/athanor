package egress

import (
	"context"
	"encoding/json"

	"github.com/tcs76321/athanor/internal/airlock/scanner"
	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/store"
)

// EventName is the closed set of `airlock` event names
// the egress exporter writes. The names are stable
// strings so log queries and audit dashboards can
// match on them.
type EventName string

const (
	EventExportComplete  EventName = "egress_complete"
	EventExportBlocked   EventName = "egress_blocked"
	EventExportUnchanged EventName = "egress_unchanged"
)

// auditExportComplete writes the `airlock` audit row
// for a clean export. The row carries the artifact
// metadata, the export path, and the per-scanner
// breakdown (size + zipbomb + clamav + yara).
func (e *Exporter) auditExportComplete(ctx context.Context, art artifact.Artifact, dst string, result scanner.PipelineResult) {
	e.writeAudit(ctx, EventExportComplete, art, dst, result, "")
}

// auditExportBlocked writes the audit row for an
// export that the scanners rejected. The reason
// string is the deciding scanner's reason.
func (e *Exporter) auditExportBlocked(ctx context.Context, art artifact.Artifact, result scanner.PipelineResult) {
	e.writeAudit(ctx, EventExportBlocked, art, "", result, result.Reason)
}

// auditExportUnchanged writes the audit row for an
// export that was a no-op (idempotent re-run, or
// non-accepted artifact). The reason string carries
// the no-op's cause.
func (e *Exporter) auditExportUnchanged(ctx context.Context, art artifact.Artifact, reason string) {
	e.writeAudit(ctx, EventExportUnchanged, art, "", scanner.PipelineResult{}, reason)
}

// writeAudit persists one event row. Errors are logged
// but not fatal; the export disposition is the durable
// record (the file on disk + the audit row).
func (e *Exporter) writeAudit(ctx context.Context, ev EventName, art artifact.Artifact, dst string, result scanner.PipelineResult, reason string) {
	data := map[string]any{
		"event":       string(ev),
		"artifact_id": art.ID,
		"project_id":  art.ProjectID,
		"kind":        string(art.Kind),
		"sha256":      art.ContentHash,
	}
	if dst != "" {
		data["stored_path"] = dst
	}
	if reason != "" {
		data["reason"] = reason
	}
	if len(result.PerScanner) > 0 {
		scanners := make(map[string]string, len(result.PerScanner))
		for _, ps := range result.PerScanner {
			scanners[ps.Scanner] = ps.Result.Verdict.String()
		}
		data["scanners"] = scanners
	}
	raw, err := json.Marshal(data)
	if err != nil {
		e.logger.Warn("egress: audit marshal failed", "event", ev, "err", err)
		return
	}
	level := store.EventInfo
	if ev == EventExportBlocked {
		level = store.EventWarn
	}
	if _, err := e.store.AppendEvent(ctx, store.Event{
		Category: "airlock",
		Level:    level,
		JobID:    art.JobID,
		Data:     map[string]any{"event": string(ev), "data": json.RawMessage(raw)},
	}); err != nil {
		e.logger.Warn("egress: audit append failed", "event", ev, "err", err)
	}
}

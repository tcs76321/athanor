-- Migration 0008: quarantined_files table (ROADMAP M4-T2; ADR-0015).
--
-- §21.3 file-airlock quarantine table. Every file the ingress
-- pipeline quarantines (path-layer rejection, scanner verdict of
-- Uncertain/Rejected, or scanner error) is recorded here keyed by
-- the SHA-256 of the file bytes. Re-ingesting the same content
-- finds the row via the primary key and is a no-op; the audit
-- trail still records the duplicate_ignored event.
--
-- The `pipeline` column distinguishes the three callers (ADR-0015
-- §"Trust boundaries, not all text"): 'ingress' for inbox-driven
-- quarantines, 'egress' for artifacts blocked at export time,
-- 'user-prompt' for long-prompt heuristic rejections on goal
-- submit. The migration's CHECK constraint makes the set closed;
-- a future pipeline adds a row by editing both this file and the
-- PipelineKind enum in internal/airlock/scanner/registry.go.
--
-- FK enforcement note (per ADR-0006): the migration runner
-- disables `PRAGMA foreign_keys` around each migration and gates
-- the commit on a `PRAGMA foreign_key_check`, so this standalone
-- table is safe to land without touching its parents.
--
-- `details` carries the per-scanner JSON verdict and timings; it
-- is opaque to SQL but human-readable in post-mortem. The
-- `reason` column carries the first failing scanner or path
-- error, machine-readable, stable string.

CREATE TABLE quarantined_files (
    sha256       TEXT PRIMARY KEY,
    relpath      TEXT NOT NULL,
    reason       TEXT NOT NULL,
    details      TEXT NOT NULL DEFAULT '{}',
    source_size  INTEGER NOT NULL,
    stored_path  TEXT NOT NULL,
    ingested_at  TEXT NOT NULL,
    pipeline     TEXT NOT NULL CHECK (pipeline IN ('ingress','egress','user-prompt')),
    job_id       TEXT
);

CREATE INDEX idx_quarantined_files_ingested_at ON quarantined_files(ingested_at);
CREATE INDEX idx_quarantined_files_pipeline     ON quarantined_files(pipeline);

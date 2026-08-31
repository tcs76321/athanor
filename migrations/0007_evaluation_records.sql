-- Migration 0007: per-candidate evaluation records (ROADMAP M3-T1; ADR-0011).
--
-- §13.1 Phase 3 (Evaluating) and §19 describe the comparison system's
-- data model: one EvaluationRecord per candidate artifact, captured at
-- temperature 0.0 by the `security` persona, capturing acceptance-
-- criteria checks, test/lint results, and the deterministic verdict
-- that Phase 6 (Comparing) consumes. The records must persist with the
-- job so the comparison phase (M3-T3) and quality probes (M3-T7) can
-- mine them without re-evaluating.
--
-- FK enforcement note: this migration creates a child table only.
-- Per ADR-0006 the migration runner disables `PRAGMA foreign_keys`
-- around each migration and gates the commit on a
-- `PRAGMA foreign_key_check`, so this table is safe to land without
-- touching its parents.

CREATE TABLE evaluation_records (
    id                     TEXT PRIMARY KEY,
    job_id                 TEXT NOT NULL REFERENCES jobs(id),
    artifact_id            TEXT NOT NULL REFERENCES artifacts(id),
    compared_against       TEXT REFERENCES artifacts(id),
    score                  REAL NOT NULL DEFAULT 0.0,
    passed_tests           INTEGER NOT NULL DEFAULT 0,
    failed_tests_json      TEXT NOT NULL DEFAULT '[]',
    missing_criteria_json  TEXT NOT NULL DEFAULT '[]',
    security_issues_json   TEXT NOT NULL DEFAULT '[]',
    style_issues_json      TEXT NOT NULL DEFAULT '[]',
    better_than_previous   INTEGER NOT NULL DEFAULT 0,
    confidence             REAL NOT NULL DEFAULT 0.0,
    summary                TEXT NOT NULL DEFAULT '',
    created_at             TEXT NOT NULL
);

CREATE INDEX idx_evaluation_records_job      ON evaluation_records(job_id, created_at);
CREATE INDEX idx_evaluation_records_artifact ON evaluation_records(artifact_id);

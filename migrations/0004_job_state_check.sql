-- Migration 0004: enforce the §8.1 job-state set at the schema layer and
-- add paused_from for resumable pauses (ROADMAP M1-T4; ADR-0006).
--
-- jobs is the first rebuilt table with INBOUND foreign keys (actions,
-- artifacts, corrections, hitl_requests all reference jobs(id)), so the
-- ADR-0005 recipe cannot be used verbatim: RENAME TO rewrites child FK
-- clauses to the new name, and deferred FKs do not survive a parent
-- DROP+RENAME. The migration runner therefore disables FK enforcement
-- around each migration (gating the commit on foreign_key_check, see
-- internal/store/migrate.go), and this migration follows SQLite's
-- documented schema-change procedure: build the new table under a
-- temporary name, copy, DROP the old table, then rename the new one into
-- place. Child references to "jobs" are never rewritten.

CREATE TABLE jobs_new (
	id            TEXT PRIMARY KEY,
	task_id       TEXT NOT NULL REFERENCES tasks(id),
	project_id    TEXT NOT NULL REFERENCES projects(id),
	state         TEXT NOT NULL DEFAULT 'queued'
	              CHECK (state IN ('queued','context_building','planning','diverging',
	                               'evaluating','reflecting','synthesizing','comparing',
	                               'awaiting_approval','paused','completed','failed','cancelled')),
	paused_from   TEXT
	              CHECK (paused_from IS NULL OR (state = 'paused' AND paused_from IN
	                                   ('context_building','planning','diverging','evaluating',
	                                    'reflecting','synthesizing','comparing'))),
	recovery_flag TEXT,
	attempt       INTEGER NOT NULL DEFAULT 0,
	strategy_profile_json TEXT NOT NULL DEFAULT '{}',
	started_at    TEXT,
	finished_at   TEXT,
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO jobs_new (id, task_id, project_id, state, recovery_flag, attempt,
                      strategy_profile_json, started_at, finished_at, created_at, updated_at)
SELECT id, task_id, project_id, state, recovery_flag, attempt,
       strategy_profile_json, started_at, finished_at, created_at, updated_at
FROM jobs;

DROP TABLE jobs;

ALTER TABLE jobs_new RENAME TO jobs;

CREATE INDEX idx_jobs_task_state ON jobs(task_id, state);

CREATE TRIGGER jobs_touch_updated_at AFTER UPDATE ON jobs
BEGIN
	UPDATE jobs SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;

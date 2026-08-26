-- Migration 0003: canonicalize enum values with ARCHITECTURE.md
-- (§9.1 artifact kinds, §18.2 correction severities, §20.2 HITL severities).
--
-- SQLite cannot alter CHECK constraints in place, so each affected table is
-- rebuilt (rename → create → copy/remap → drop old → restore index) inside
-- this migration's transaction. PRAGMA defer_foreign_keys makes row-copy
-- ordering irrelevant; enforcement is verified at COMMIT.
--
-- Remapping (deterministic; tables had no write paths yet):
--   corrections.severity:   minor → low, major → medium, critical → critical (§18.2)
--   hitl_requests.severity: info → low, normal → medium (§20.2)
--   artifacts.kind:         'media' and 'configuration' added (§9.1)

PRAGMA defer_foreign_keys = ON;

-- ---------------------------------------------------------------------------
-- corrections: severity low|medium|high|critical (§18.2)
-- ---------------------------------------------------------------------------
ALTER TABLE corrections RENAME TO corrections_old;

CREATE TABLE corrections (
	id            TEXT PRIMARY KEY,
	project_id    TEXT REFERENCES projects(id),
	source_job_id TEXT REFERENCES jobs(id),
	category      TEXT NOT NULL,
	severity      TEXT NOT NULL DEFAULT 'low'
	              CHECK (severity IN ('low','medium','high','critical')),
	derived_rule  TEXT NOT NULL,
	context_json  TEXT NOT NULL DEFAULT '{}',
	status        TEXT NOT NULL DEFAULT 'active'
	              CHECK (status IN ('active','muted','expired')),
	usage_count   INTEGER NOT NULL DEFAULT 0,
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO corrections
SELECT id, project_id, source_job_id, category,
       CASE severity WHEN 'minor' THEN 'low'
                     WHEN 'major' THEN 'medium'
                     ELSE severity END,
       derived_rule, context_json, status, usage_count,
       created_at, updated_at
FROM corrections_old;

DROP TABLE corrections_old;

CREATE INDEX idx_corrections_project_status ON corrections(project_id, status);

-- ---------------------------------------------------------------------------
-- hitl_requests: severity low|medium|high|critical (§20.2)
-- ---------------------------------------------------------------------------
ALTER TABLE hitl_requests RENAME TO hitl_requests_old;

CREATE TABLE hitl_requests (
	id            TEXT PRIMARY KEY,
	project_id    TEXT REFERENCES projects(id),
	job_id        TEXT REFERENCES jobs(id),
	type          TEXT NOT NULL,
	severity      TEXT NOT NULL DEFAULT 'medium'
	              CHECK (severity IN ('low','medium','high','critical')),
	payload_json  TEXT NOT NULL DEFAULT '{}',
	status        TEXT NOT NULL DEFAULT 'pending'
	              CHECK (status IN ('pending','approved','rejected','expired','cancelled')),
	decision_note TEXT,
	expires_at    TEXT,
	decided_at    TEXT,
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO hitl_requests
SELECT id, project_id, job_id, type,
       CASE severity WHEN 'info' THEN 'low'
                     WHEN 'normal' THEN 'medium'
                     ELSE severity END,
       payload_json, status, decision_note, expires_at, decided_at,
       created_at, updated_at
FROM hitl_requests_old;

DROP TABLE hitl_requests_old;

CREATE INDEX idx_hitl_status ON hitl_requests(status, expires_at);

-- ---------------------------------------------------------------------------
-- artifacts: kind gains 'media' and 'configuration' (§9.1)
-- ---------------------------------------------------------------------------
ALTER TABLE artifacts RENAME TO artifacts_old;

CREATE TABLE artifacts (
	id           TEXT PRIMARY KEY,
	project_id   TEXT NOT NULL REFERENCES projects(id),
	task_id      TEXT REFERENCES tasks(id),
	job_id       TEXT REFERENCES jobs(id),
	supersedes_id TEXT REFERENCES artifacts(id),
	kind         TEXT NOT NULL CHECK (kind IN ('code','document','dataset','proposal','evaluation','media','configuration')),
	version      INTEGER NOT NULL DEFAULT 1,
	status       TEXT NOT NULL DEFAULT 'draft'
	             CHECK (status IN ('draft','candidate','accepted','rejected','quarantined','superseded')),
	storage_path TEXT,
	content_hash TEXT,
	created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO artifacts SELECT * FROM artifacts_old;

DROP TABLE artifacts_old;

CREATE INDEX idx_artifacts_project ON artifacts(project_id, kind, status);

CREATE TRIGGER artifacts_touch_updated_at AFTER UPDATE ON artifacts
BEGIN
	UPDATE artifacts SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;

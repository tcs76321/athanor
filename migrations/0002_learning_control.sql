-- Migration 0002: learning & control tables (ROADMAP M0-T5b;
-- ARCHITECTURE §5, §18, §20). Same standards as 0001; forward-only.

CREATE TABLE corrections (
	id            TEXT PRIMARY KEY,
	project_id    TEXT REFERENCES projects(id),
	source_job_id TEXT REFERENCES jobs(id),
	category      TEXT NOT NULL,
	severity      TEXT NOT NULL DEFAULT 'minor'
	              CHECK (severity IN ('minor','major','critical')),
	derived_rule  TEXT NOT NULL,
	context_json  TEXT NOT NULL DEFAULT '{}',
	status        TEXT NOT NULL DEFAULT 'active'
	              CHECK (status IN ('active','muted','expired')),
	usage_count   INTEGER NOT NULL DEFAULT 0,
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE hitl_requests (
	id            TEXT PRIMARY KEY,
	project_id    TEXT REFERENCES projects(id),
	job_id        TEXT REFERENCES jobs(id),
	type          TEXT NOT NULL,
	severity      TEXT NOT NULL DEFAULT 'normal'
	              CHECK (severity IN ('info','normal','high','critical')),
	payload_json  TEXT NOT NULL DEFAULT '{}',
	status        TEXT NOT NULL DEFAULT 'pending'
	              CHECK (status IN ('pending','approved','rejected','expired','cancelled')),
	decision_note TEXT,
	expires_at    TEXT,
	decided_at    TEXT,
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE prompt_templates (
	id         TEXT PRIMARY KEY,
	tier       TEXT NOT NULL CHECK (tier IN ('static','runtime','user','meta','skill','eval')),
	name       TEXT NOT NULL,
	version    INTEGER NOT NULL,
	template   TEXT NOT NULL,
	checksum   TEXT NOT NULL,
	is_active  INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	UNIQUE (name, version)
);

CREATE TABLE personas (
	role           TEXT PRIMARY KEY
	               CHECK (role IN ('wide','tall','main','security','alternative')),
	model          TEXT NOT NULL,
	context_target INTEGER NOT NULL CHECK (context_target > 0),
	temperature    REAL NOT NULL CHECK (temperature >= 0 AND temperature <= 2),
	updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX idx_corrections_project_status ON corrections(project_id, status);
CREATE INDEX idx_hitl_status                ON hitl_requests(status, expires_at);
CREATE INDEX idx_prompt_templates_active    ON prompt_templates(tier, name, is_active);

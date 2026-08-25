-- Migration 0001: execution spine (ROADMAP M0-T5a; ARCHITECTURE §5, §23).
-- IDs are application-generated UUIDs; timestamps are UTC RFC3339.
-- updated_at is maintained by per-table triggers.

CREATE TABLE projects (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL UNIQUE,
	archetype   TEXT NOT NULL CHECK (archetype IN ('text','code','document','data','media')),
	goal        TEXT NOT NULL,
	status      TEXT NOT NULL DEFAULT 'active'
	            CHECK (status IN ('active','paused','archived')),
	preferences_json   TEXT NOT NULL DEFAULT '{}',
	created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE goals (
	id          TEXT PRIMARY KEY,
	project_id  TEXT NOT NULL REFERENCES projects(id),
	text        TEXT NOT NULL CHECK (length(text) BETWEEN 20 AND 500),
	status      TEXT NOT NULL DEFAULT 'open'
	            CHECK (status IN ('open','decomposed','completed','failed')),
	created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE tasks (
	id               TEXT PRIMARY KEY,
	project_id       TEXT NOT NULL REFERENCES projects(id),
	goal_id          TEXT REFERENCES goals(id),
	parent_task_id   TEXT REFERENCES tasks(id),
	title            TEXT NOT NULL,
	description      TEXT NOT NULL DEFAULT '',
	status           TEXT NOT NULL DEFAULT 'pending'
	                 CHECK (status IN ('pending','ready','in_progress','blocked','done','failed','cancelled')),
	priority         INTEGER NOT NULL DEFAULT 0,
	depends_on_json  TEXT NOT NULL DEFAULT '[]',
	acceptance_criteria_json TEXT NOT NULL DEFAULT '[]',
	budget_json      TEXT NOT NULL DEFAULT '{}',
	created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE jobs (
	id            TEXT PRIMARY KEY,
	task_id       TEXT NOT NULL REFERENCES tasks(id),
	project_id    TEXT NOT NULL REFERENCES projects(id),
	state         TEXT NOT NULL DEFAULT 'queued',
	recovery_flag TEXT,
	attempt       INTEGER NOT NULL DEFAULT 0,
	strategy_profile_json TEXT NOT NULL DEFAULT '{}',
	started_at    TEXT,
	finished_at   TEXT,
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE actions (
	id            TEXT PRIMARY KEY,
	job_id        TEXT NOT NULL REFERENCES jobs(id),
	seq           INTEGER NOT NULL,
	tool          TEXT NOT NULL,
	request_json  TEXT NOT NULL DEFAULT '{}',
	response_json TEXT,
	status        TEXT NOT NULL DEFAULT 'pending'
	              CHECK (status IN ('pending','running','succeeded','failed','denied')),
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	UNIQUE (job_id, seq)
);

CREATE TABLE artifacts (
	id           TEXT PRIMARY KEY,
	project_id   TEXT NOT NULL REFERENCES projects(id),
	task_id      TEXT REFERENCES tasks(id),
	job_id       TEXT REFERENCES jobs(id),
	supersedes_id TEXT REFERENCES artifacts(id),
	kind         TEXT NOT NULL CHECK (kind IN ('code','document','dataset','proposal','evaluation')),
	version      INTEGER NOT NULL DEFAULT 1,
	status       TEXT NOT NULL DEFAULT 'draft'
	             CHECK (status IN ('draft','candidate','accepted','rejected','quarantined','superseded')),
	storage_path TEXT,
	content_hash TEXT,
	created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Append-only audit trail (§28). No UPDATE/DELETE paths exist at the API
-- layer; the trigger below makes it enforceable at the database layer too.
CREATE TABLE events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	ts         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	category   TEXT NOT NULL,
	level      TEXT NOT NULL DEFAULT 'info'
	           CHECK (level IN ('debug','info','warn','error')),
	project_id TEXT,
	job_id     TEXT,
	data_json  TEXT NOT NULL DEFAULT '{}'
);

CREATE TRIGGER events_no_update
BEFORE UPDATE ON events
BEGIN
	SELECT RAISE(ABORT, 'events table is append-only');
END;

CREATE TRIGGER events_no_delete
BEFORE DELETE ON events
BEGIN
	SELECT RAISE(ABORT, 'events table is append-only');
END;

CREATE INDEX idx_tasks_project_status ON tasks(project_id, status);
CREATE INDEX idx_jobs_task_state      ON jobs(task_id, state);
CREATE INDEX idx_artifacts_project    ON artifacts(project_id, kind, status);
CREATE INDEX idx_events_category_ts   ON events(category, ts);
CREATE INDEX idx_events_job           ON events(job_id);

-- updated_at maintenance triggers (M0-T5a acceptance: triggers present).
CREATE TRIGGER projects_touch_updated_at AFTER UPDATE ON projects
BEGIN
	UPDATE projects SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;

CREATE TRIGGER goals_touch_updated_at AFTER UPDATE ON goals
BEGIN
	UPDATE goals SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;

CREATE TRIGGER tasks_touch_updated_at AFTER UPDATE ON tasks
BEGIN
	UPDATE tasks SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;

CREATE TRIGGER jobs_touch_updated_at AFTER UPDATE ON jobs
BEGIN
	UPDATE jobs SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;

CREATE TRIGGER actions_touch_updated_at AFTER UPDATE ON actions
BEGIN
	UPDATE actions SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;

CREATE TRIGGER artifacts_touch_updated_at AFTER UPDATE ON artifacts
BEGIN
	UPDATE artifacts SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id;
END;

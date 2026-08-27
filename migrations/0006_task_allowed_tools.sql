-- Migration 0006: per-task tool allowlist override.
--
-- ROADMAP M2-T4: tasks gain an optional allowed_tools_json column
-- that overrides config.job_pod.default_tools for a single task.
-- A non-empty value wins; an empty value (the default) falls back
-- to the config default. The merge is performed in
-- (*project.Repo).EnvelopeFor, not in the SQL layer, so the
-- per-task override can be empty without a NULL.
--
-- Per ARCHITECTURE.md §5 the migrations are forward-only, so the
-- column and its default land together. The closed-set membership
-- check (which tools are allowed) is enforced in
-- internal/toolenvelope.Parse at config-load time and again at
-- read time in (*project.Repo).EnvelopeFor; no SQL CHECK is
-- added because ALTER TABLE ADD COLUMN does not support a
-- table-level CHECK constraint in SQLite (see sqlite.org/altertable).

ALTER TABLE tasks ADD COLUMN allowed_tools_json TEXT NOT NULL DEFAULT '[]';

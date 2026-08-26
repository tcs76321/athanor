-- Migration 0005: system_state key-value table for daemon-wide control
-- flags (ROADMAP M1-T6; ARCHITECTURE §22). The kill switch's frozen flag
-- must survive restarts (§22.2: frozen mode never lifts automatically),
-- so it lives in the database, not in process memory.

CREATE TABLE system_state (
	key         TEXT PRIMARY KEY,
	value       TEXT NOT NULL,
	updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TRIGGER system_state_touch_updated_at AFTER UPDATE ON system_state
BEGIN
	UPDATE system_state SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE key = NEW.key;
END;

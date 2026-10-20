-- Redacted hook events of each session. Ingest merges them with new spool
-- events, so a second ingest of the same session sees all events.
CREATE TABLE hook_events (session_id TEXT, key TEXT, seq INTEGER, ts TEXT, event TEXT,
                          payload TEXT /*redacted json*/, snapshot TEXT,
                          PRIMARY KEY (session_id, key));
CREATE INDEX idx_hook_events_session_seq ON hook_events(session_id, seq);

-- Suppressions survive a new computation of the flags of a change. A flag
-- is suppressed when its fingerprint has a row here.
CREATE TABLE suppressions (fingerprint TEXT PRIMARY KEY, flag_id TEXT, rule_id TEXT,
                           reason TEXT, created_at TEXT);
ALTER TABLE flags ADD COLUMN fingerprint TEXT;
CREATE INDEX idx_flags_fingerprint ON flags(fingerprint);

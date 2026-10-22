-- The review status of each hunk, by tui.HunkKey: the file and the content of
-- the hunk. xis writes the hunks of a change again on each run, so the status
-- has its own table, and it stays while the hunk content stays.
CREATE TABLE hunk_status (hunk_key TEXT PRIMARY KEY, status TEXT NOT NULL, updated_at TEXT);

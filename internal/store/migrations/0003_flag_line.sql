-- The line in the new file that a hunk flag points to. 0 means the start of the hunk.
ALTER TABLE flags ADD COLUMN line INTEGER DEFAULT 0;

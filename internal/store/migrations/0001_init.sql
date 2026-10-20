CREATE TABLE repos      (id TEXT PRIMARY KEY, path TEXT, remote_url TEXT, created_at TEXT);
CREATE TABLE sessions   (id TEXT PRIMARY KEY, repo_id TEXT, agent TEXT, agent_version TEXT,
                         agent_session_id TEXT, models TEXT /*json array*/,
                         fidelity TEXT CHECK (fidelity IN ('live','imported')),
                         base_commit TEXT, base_snapshot TEXT, final_snapshot TEXT,
                         started_at TEXT, ended_at TEXT, cwd TEXT, title TEXT,
                         parent_session_id TEXT, no_git INTEGER DEFAULT 0,
                         input_tokens INTEGER, output_tokens INTEGER, status TEXT);
CREATE TABLE actors     (id TEXT PRIMARY KEY, session_id TEXT, kind TEXT /*agent|subagent|human*/,
                         name TEXT, parent_actor_id TEXT);
CREATE TABLE steps      (id TEXT PRIMARY KEY, session_id TEXT, actor_id TEXT, parent_step_id TEXT,
                         seq INTEGER, ts_start TEXT, ts_end TEXT,
                         kind TEXT /*user_message|assistant_message|reasoning|plan|tool_call|compaction|human_edit*/,
                         text TEXT /*redacted*/, snapshot TEXT /*shadow commit after this step, nullable*/,
                         summary TEXT /*LLM, nullable*/);
CREATE TABLE tool_calls (step_id TEXT PRIMARY KEY, tool_use_id TEXT, tool_name TEXT,
                         category TEXT /*read|search|edit|shell|test|mcp|web|other*/,
                         input TEXT /*redacted json*/, output TEXT /*redacted, truncated*/,
                         exit_code INTEGER, ok INTEGER, duration_ms INTEGER);
CREATE TABLE edits      (id TEXT PRIMARY KEY, step_id TEXT, file_path TEXT, old_path TEXT,
                         before_blob TEXT, after_blob TEXT, patch TEXT, added INTEGER, removed INTEGER,
                         reverted_by_edit_id TEXT);
CREATE TABLE test_runs  (step_id TEXT PRIMARY KEY, command TEXT, framework TEXT,
                         passed INTEGER, failed INTEGER, skipped INTEGER,
                         outcome TEXT /*pass|fail|error|unknown*/);
CREATE TABLE intent_items (id TEXT PRIMARY KEY, session_id TEXT, source TEXT /*prompt|followup|plan|todo*/,
                         source_step_id TEXT, text TEXT, status TEXT /*addressed|partial|unaddressed|unknown*/);
CREATE TABLE changes    (id TEXT PRIMARY KEY, repo_id TEXT, base_ref TEXT, target_ref TEXT,
                         session_ids TEXT /*json*/, created_at TEXT);
CREATE TABLE hunks      (id TEXT PRIMARY KEY, change_id TEXT, file_path TEXT, old_start INTEGER,
                         old_lines INTEGER, new_start INTEGER, new_lines INTEGER, patch TEXT,
                         change_type TEXT /*behavior|refactor|test|config|dependency|docs|generated|unknown*/,
                         review_unit_id TEXT, content_hash TEXT);
CREATE TABLE hunk_provenance (hunk_id TEXT, step_id TEXT, lines INTEGER, PRIMARY KEY (hunk_id, step_id));
CREATE TABLE hunk_intent (hunk_id TEXT, intent_item_id TEXT, confidence REAL, PRIMARY KEY (hunk_id, intent_item_id));
CREATE TABLE rationales (hunk_id TEXT PRIMARY KEY, agent_claim TEXT, claim_step_id TEXT,
                         explanation TEXT, evidence_step_ids TEXT /*json*/, verified INTEGER,
                         model TEXT, created_at TEXT);
CREATE TABLE review_units (id TEXT PRIMARY KEY, change_id TEXT, title TEXT, ord INTEGER,
                         status TEXT /*unreviewed|understood|questioned*/, source TEXT /*rule|llm*/);
CREATE TABLE flags      (id TEXT PRIMARY KEY, change_id TEXT, session_id TEXT, rule_id TEXT,
                         severity TEXT /*info|low|medium|high*/, detector TEXT /*rule|llm*/,
                         target_kind TEXT /*hunk|step|session|file*/, target_id TEXT,
                         message TEXT, evidence_step_ids TEXT /*json*/, suppressed INTEGER DEFAULT 0);
CREATE TABLE qa         (id TEXT PRIMARY KEY, change_id TEXT, question TEXT, answer TEXT,
                         cited_step_ids TEXT, cited_hunk_ids TEXT, model TEXT, created_at TEXT);
CREATE TABLE blobs      (sha256 TEXT PRIMARY KEY, size INTEGER, content BLOB /*small redacted text only*/);

CREATE INDEX idx_sessions_repo ON sessions(repo_id);
CREATE INDEX idx_sessions_agent_session ON sessions(agent, agent_session_id);
CREATE INDEX idx_sessions_parent ON sessions(parent_session_id);
CREATE INDEX idx_actors_session ON actors(session_id);
CREATE INDEX idx_steps_session_seq ON steps(session_id, seq);
CREATE INDEX idx_steps_actor ON steps(actor_id);
CREATE INDEX idx_steps_parent ON steps(parent_step_id);
CREATE INDEX idx_steps_snapshot ON steps(snapshot);
CREATE INDEX idx_tool_calls_tool_use ON tool_calls(tool_use_id);
CREATE INDEX idx_edits_step ON edits(step_id);
CREATE INDEX idx_edits_file ON edits(file_path);
CREATE INDEX idx_intent_session ON intent_items(session_id);
CREATE INDEX idx_intent_step ON intent_items(source_step_id);
CREATE INDEX idx_changes_repo ON changes(repo_id);
CREATE INDEX idx_hunks_change ON hunks(change_id);
CREATE INDEX idx_hunks_file ON hunks(file_path);
CREATE INDEX idx_hunks_unit ON hunks(review_unit_id);
CREATE INDEX idx_hunk_prov_step ON hunk_provenance(step_id);
CREATE INDEX idx_hunk_intent_item ON hunk_intent(intent_item_id);
CREATE INDEX idx_rationales_claim_step ON rationales(claim_step_id);
CREATE INDEX idx_review_units_change ON review_units(change_id);
CREATE INDEX idx_flags_change ON flags(change_id);
CREATE INDEX idx_flags_session ON flags(session_id);
CREATE INDEX idx_flags_target ON flags(target_kind, target_id);
CREATE INDEX idx_qa_change ON qa(change_id);

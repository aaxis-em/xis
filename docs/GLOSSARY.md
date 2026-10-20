# Glossary

This document defines the project terms. Each term has one meaning only. The other documents, the CLI help and the code use these terms exactly.

| Term | Meaning |
| --- | --- |
| **agent** | A program that edits code for a developer, for example Claude Code or Codex CLI. |
| **session** | One run of an agent in one repository, from start to end. A session has an ID that the agent gives and an ID that xis gives. |
| **actor** | The part of a session that did a step: the main agent, a subagent or the human. |
| **step** | One event in a session, in time order. Kinds: `user_message`, `assistant_message`, `reasoning`, `plan`, `tool_call`, `compaction` and `human_edit`. |
| **tool call** | A step in which the agent used a tool, for example `Edit`, `Bash` or `Read`. |
| **snapshot** | The full state of the working tree at one time. xis stores each snapshot as a commit in the shadow repository. |
| **shadow repository** | A bare git repository that xis keeps in its data directory. It holds the snapshots. It never changes your repository. |
| **spool** | A file that holds the hook events of one session until ingest reads them. |
| **ingest** | The process that reads the spool and the transcript, removes secrets and writes the session to the database. |
| **transcript** | The log file that the agent writes for a session. |
| **edit** | The difference in one file between two consecutive snapshots. Each edit belongs to the step that made the later snapshot. |
| **change** | The code difference that you review. It has a base and a target, for example the commit at session start and the working tree. |
| **hunk** | One block of changed lines in a change, as `git diff` shows it. |
| **provenance** | The link from each line of a hunk to the step that wrote it. |
| **contributor** | The source of a hunk: `ai`, `human`, `mixed` or `unknown`. |
| **review unit** | A group of related hunks that you review together. |
| **flag** | A finding of a rule (or of the LLM) about a risk in a change. A flag has a severity: `info`, `low`, `medium` or `high`. |
| **rule** | A fixed check that makes flags from stored data. A rule never uses an LLM. |
| **intent item** | One thing that the user asked for, from a prompt, a follow-up, a plan or a todo list. |
| **fidelity** | How xis captured a session. `live` means hooks and snapshots. `imported` means xis read a log after the session. |
| **evidence** | Recorded facts that support or refute a statement: files read, commands run and test results. |
| **agent claim** | A statement that the agent made about its own work. xis shows it as "agent says" and marks it `verified` or `unverified`. |
| **test run** | A tool call that ran tests. Its outcome is `pass`, `fail`, `error` or `unknown`. |
| **scope** | The set of paths that the session was about. The `OUT_OF_SCOPE` rule uses it. |
| **note** | The signed provenance record that xis attaches to a commit under `refs/notes/xis`. |
| **redaction** | The replacement of a secret with a token such as `[REDACTED:aws-access-token:1a2b]`. |

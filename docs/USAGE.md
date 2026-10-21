# Usage

This document describes each xis command with examples.

## `xis init`

The command creates the data directories, the config file, the database and the signing key. Run it once.

```console
$ xis init
created /home/me/.config/xis/config.toml
database /home/me/.local/share/xis/xis.db
created signing key in /home/me/.local/share/xis/keys

Next steps:
  1. In your repository, run: xis hook install --agent claude-code
  2. Use Claude Code as usual.
  3. Run: xis review
```

## `xis hook install` and `xis hook uninstall`

The command adds the xis hooks to the Claude Code settings. The default scope is `project` (`.claude/settings.json` in the repository). Use `--scope user` for `~/.claude/settings.json`.

```console
$ xis hook install --agent claude-code
/work/repo/.claude/settings.json:
  added SessionStart hook: /usr/local/bin/xis hook claude-code SessionStart
  ...
$ xis hook install --agent claude-code
/work/repo/.claude/settings.json: no change
```

The command keeps all other settings and hooks. A second run changes nothing. `xis hook uninstall` removes only the xis hooks.

## `xis doctor`

The command checks git, the config, the permissions, the database, the hooks, the spool, the LLM config and the signing key.

```console
$ xis doctor
ok    git            git 2.43.0
ok    config         /home/me/.config/xis/config.toml
ok    permissions    /home/me/.local/share/xis (0700/0600)
ok    database       /home/me/.local/share/xis/xis.db (2 migrations, 3 sessions)
ok    hooks          installed (project)
ok    spool          empty
ok    llm            off (provider none). Rules, why and flags work without it.
ok    signing key    /home/me/.local/share/xis/keys
```

The exit code is 1 if a check fails.

## `xis version`

The command shows the version, the commit and the build date.

```console
$ xis version
xis v0.1.0
commit: 1a2b3c4
built: 2026-10-20T10:00:00Z
go: go1.26.4 linux/amd64
```

## `xis config`

The command shows and changes the config.

```console
$ xis config path
config:      /home/me/.config/xis/config.toml
data:        /home/me/.local/share/xis
$ xis config set llm.provider none
$ xis config get llm.provider
none
```

## `xis ingest`

The hooks start ingest automatically when a session stops. The command `xis ingest` ingests all spooled sessions. Use `--session ID` for one session. Other commands ingest pending sessions before they start.

```console
$ xis ingest
ingested 01M5CRVYT5 (agent session 9287fccd-7bef-438f-9387-8921f229846e): 8 steps, 3 edits, 1 test runs
```

## `xis sessions`

The command lists the sessions of the repository, the newest first. Use `--all` for all repositories and `--limit N` for more rows.

```console
$ xis sessions
ID          AGENT        FIDELITY  STARTED           DURATION  STEPS  FILES  FLAGS (H/M/L)  STATUS
01M5CRVYT5  claude-code  live      2026-10-20 19:28  12s       8      2      0/0/0          ended
```

## `xis timeline`

The command prints the steps of a session. The command indents subagent steps. For a step with edits, it shows how many hunks of the change survive. A step whose edits do not survive is "abandoned".

```console
$ xis timeline
session 01M5CRVYT5 · claude-code 2.1.304 (live) · 8 steps · 2026-10-20 19:28
   1     +2s user_message Read calc.go. Add a function Sub(a, b int) int ...
   2     +4s tool      Read calc.go
   4     +7s tool      Edit calc.go  → calc.go +3 -0 · 1 hunks survive
   6     +9s tool      Bash: go test ./... [test PASS]
   7    +11s tool      Bash: sed -i 's/returns a + b/.../' calc.go  → calc.go +1 -1 · 1 hunks survive
   8    +13s assistant_message Yes, the tests pass.
```

## `xis why`

The command shows the origin of one line of the working tree. It needs no LLM.

```console
$ xis why calc.go:3
calc.go:3
  session      01M5CRVYT5 · claude-code (live)
  hunk         @@ -1,4 +1,7 @@ [behavior] · contributor ai
  step         #7 Bash: sed -i 's/returns a + b/returns the sum of a and b/' calc.go (agent)
  instruction  #1 "Read calc.go. Add a function Sub(a, b int) int that returns a - b ..."
  evidence:
    #2 Read calc.go
    #6 Bash: go test ./... → PASS
  explanation  LLM analysis off or not run
```

The step can be a `Bash` call. xis sees edits from shell commands, because it takes a snapshot after each mutating tool call.

## `xis flags`

The command runs the rules on a change and prints the flags. The rules need no LLM. See [RULES.md](RULES.md) for each rule.

```console
$ xis flags
SEVERITY  RULE                     LOCATION      MESSAGE                                                         ID
high      TEST_EDIT_AFTER_FAILURE  step 12       The agent edited the test file calc_test.go after a failing...  01M5D2K9QZ
medium    TEST_FILE_MODIFIED       calc_test.go  The agent changed the test file calc_test.go.                   01M5D2K9R4
```

Use the change options to select the change:

| Option | Change |
| --- | --- |
| (none) | the latest session against the working tree |
| `SESSION` | that session against the working tree |
| `--staged` | `HEAD` against the index |
| `--range A..B` | commit `A` against commit `B` |
| `--pr` | the merge base with the default branch against `HEAD` |

The exit code is 0, 1 or 2:

| Exit code | Meaning |
| --- | --- |
| 0 | No unsuppressed flag at or above `--fail-on` (default `high`). |
| 1 | One or more such flags. |
| 2 | An xis error. |

Use `--json` for machine output.

## `xis flags suppress`

The command accepts one flag with a reason. A unique prefix of the flag ID is enough. The suppression also applies to the same finding in later runs.

```console
$ xis flags suppress 01M5D2K9QZ --reason "The old test expected the bug."
suppressed TEST_EDIT_AFTER_FAILURE 01M5D2K9QZ: The old test expected the bug.
```

## `xis precommit install`

The command installs a git pre-commit hook. The hook runs `xis flags --staged --fail-on high`. A high-severity flag stops the commit. An xis error does not stop the commit.

```console
$ xis precommit install
/work/repo/.git/hooks/pre-commit: installed (runs the existing hook pre-commit.xis-orig first)
```

The command keeps an existing hook. It renames the hook to `pre-commit.xis-orig`, and the xis hook runs it first. `xis precommit uninstall` removes the xis hook and restores the original hook. The command respects `core.hooksPath`.

To commit once without the check, run `git commit --no-verify`.

## `xis review`

The command opens the review UI for a change. It takes the same change options as `xis flags`. The UI works without an LLM.

```text
┌ xis · fix-login-timeout · claude-code (live) · 42 steps · 7 files ──────── 1 high · 1 med ┐
│ UNITS                │ calc.go  [behavior]  hunk 1/2      │ WHY                          │
│ ▸ 1 Behavior: 2 files│ @@ -1,5 +1,6 @@                    │ Step 4 · Edit calc.go        │
│   2 Tests  ⚑         │ +// Add returns the sum of a and b.│ Agent text near this edit:   │
│   3 Docs: README.md  │ -    return a - b                  │  "The bug is in Add: ..."    │
│                      │ +    return a + b                  │ Evidence: Read calc.go (s3)  │
└──────────────────────┴────────────────────────────────────┴──────────────────────────────┘
```

The UI has three panes:

| Pane | Content |
| --- | --- |
| UNITS | The review units in review order, their status (✔ understood, ? questioned) and their flags. Below them: the intent items, or the prompts if no LLM ran. |
| DIFF | The current hunk with syntax colors, and the flags of the hunk. |
| WHY | The steps that wrote the hunk, the agent text near the edit, the instruction, the evidence and the explanation. |

The layout depends on the terminal width:

| Width | Layout |
| --- | --- |
| 140 columns or more | Three panes. |
| 100 to 139 columns | Two panes: UNITS and DIFF. Press `tab` to show WHY in place of DIFF. |
| Less than 100 columns | One pane. Press `tab` to go to the next pane. |

The keys are:

| Key | Action |
| --- | --- |
| `j` / `k` | Move down or up in the focused pane. In DIFF, scroll a long hunk, then go to the next hunk. |
| `tab` | Focus the next pane. |
| `enter` | Expand: from UNITS to DIFF, from DIFF to WHY. |
| `[` / `]` | Go to the previous or the next review unit. |
| `g` / `G` | Go to the top or the bottom. |
| `n` / `N` | Go to the next or the previous flag. |
| `/` | Search the file paths and the hunks. |
| `u` | Mark the unit as understood. Press again to clear. |
| `x` | Mark the unit as questioned. Press again to clear. |
| `t` | Open the timeline. Press `enter` on a step to see its full (redacted) content. |
| `f` | Open the list of flags. Press `enter` to go to the hunk of a flag. |
| `a` | Ask a question. This needs an LLM. |
| `o` | Open the file at the hunk in `$VISUAL` or `$EDITOR`. |
| `?` | Show the help. |
| `q` | Quit, or go back from a view. |

xis stores the status of each unit in the database. The status stays when you open the review again.

Without an LLM, the WHY pane says "agent text near this edit", not "rationale". The text is what the agent wrote before the edit. It is a claim, not a fact.

## `xis analyze`

The command runs the LLM analysis of a change. It needs an LLM provider (see [CONFIG.md](CONFIG.md)). It takes the same change options as `xis flags`.

```console
$ xis analyze
model claude-sonnet-5-5 · 5 calls · 0 cached · estimated $0.0412
intent items: 1
  #1 [addressed] Fix the Add function in calc.go
review units: 2 (LLM)
  1 Fix addition
  2 Update the Add test
hunk explanations: 4 of 4 hunks
```

The analysis has four tasks:

1. Intent: the things that the user asked for. The sources are the prompts, the follow-up prompts, the plans and the todo lists. The agent's own summaries are never a source.
2. Grouping: the review units. Every hunk must be in exactly one unit. Otherwise xis keeps the fixed grouping.
3. Hunk explanation: a verbatim agent claim, a plain explanation, the evidence and the intent items of each hunk.
4. Intent status: `addressed`, `partial` or `unaddressed` for each intent item.

xis checks each result before it stores it:

- Every sentence of an explanation must cite a valid `[step:ID]` or `[hunk:ID]`. xis deletes the other sentences. If no sentence stays, xis shows "No evidence-backed explanation."
- An agent claim must be an exact quote from an agent step near the edit. Otherwise xis drops it.
- An agent claim is `verified` only if xis checked it against the record. In v1, xis checks claims about test results and about edited files. All other claims are `unverified`.

xis caches each response in `~/.local/share/xis/cache/llm`. A second run of the same analysis costs nothing. The analysis stops when the estimated cost gets to `llm.max_usd_per_change`.

## `xis ask`

The command answers a question about a change from the session record. Every sentence of the answer cites a step or a hunk.

```console
$ xis ask "Why did the agent change the test?"
The test expected the old, wrong sum, so the agent changed the test after the failing run [step:01M5CTQV4E8Y0J5Q3W2K7N1B6D].

citations:
  [step:01M5CTQV4E8Y0J5Q3W2K7N1B6D] step 11 The test expects the old value. I will update the test.
```

If the record has no answer, the answer is "Not in the session record." In `xis review`, press `a` to ask. Press `tab` to select a citation and `enter` to go to it.

## `xis report`

The command writes a summary of a change for a pull request. The default format is Markdown. Use the global `--json` flag for JSON, and `-o FILE` to write a file.

```console
$ xis report --md -o report.md
wrote report.md
```

The report has these sections:

1. A header with the agents, the models, the fidelity, the steps, the duration and the changed files and lines.
2. Intent: the intent items and their status.
3. Flags: a table of the unsuppressed flags. Then the suppressed flags with their reasons.
4. Review units: the units in review order, with a short explanation and the files of each unit. Without an LLM, the explanation is a provenance summary.
5. Verification: the test runs, the status of the last run and the unverified agent claims.
6. A footer with the xis version, the session IDs and the note reference.

The report has about 150 lines. Use `--full` for all items. The report contains no secrets and no transcript text, except short quotes of agent claims.

## `xis note`

The command attaches a signed provenance note to a commit, under `refs/notes/xis`.

```console
$ git commit -m "Fix add"
$ xis note write
wrote refs/notes/xis note for 3f786850e387 (1 sessions, 4 hunks)
$ xis note verify
valid (signed with the local key)
```

The note contains only hashes, counts and IDs. It has the sessions, the flag counts and the review counts. For each hunk, it has a content hash and a contributor. It contains no transcript text, no prompts and no code. xis signs the note with the ed25519 key in `~/.local/share/xis/keys`.

`xis note verify` exits with code 1 if the note changed after it was signed. `xis note show` shows the note and the signature status.

To write a note after each commit automatically, set `notes.auto = true` and run `xis precommit install`. The command then also installs a `post-commit` hook.

Git does not push notes by default. To share the notes, run:

```sh
git push origin refs/notes/xis
```

## `xis export agent-trace`

The command writes the change as an [Agent Trace](https://agent-trace.dev/) v0.1 record (JSON). Each file has one conversation for each source: the AI lines of each session, the human lines and the unknown lines. Each range has a `murmur3:` content hash.

```console
$ xis export agent-trace --range HEAD~1..HEAD -o trace.json
wrote trace.json
```

For a commit range, `vcs.revision` is the target commit. For the working tree, the record has no `vcs` object, because the lines refer to no commit.

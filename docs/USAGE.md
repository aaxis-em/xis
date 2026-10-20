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

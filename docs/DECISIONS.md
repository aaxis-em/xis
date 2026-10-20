# Decisions

This log records each decision that the specification does not settle. Each entry gives the context, the choice and the reason.

## D1. Go version

- **Context:** The specification asks for Go 1.23 or later. The current `modernc.org/sqlite` needs Go 1.26.
- **Choice:** `go.mod` declares the Go version that the dependencies need.
- **Why:** An old SQLite driver would miss fixes. Go 1.26 still satisfies "1.23 or later".

## D2. TOML library

- **Context:** The config file is TOML. The specification names no TOML library.
- **Choice:** `github.com/BurntSushi/toml`.
- **Why:** It is the most used Go TOML library, it has no dependencies and it can decode into existing structs. That gives a simple merge of defaults, user file and repo file.

## D3. ULID library

- **Context:** Section 8 asks for ULIDs.
- **Choice:** `github.com/oklog/ulid/v2`, as Section 8 names.
- **Why:** The specification names it.

## D4. Value "auto" for `llm.provider`

- **Context:** Section 12.1 says that `none` is the default when no key is found.
- **Choice:** The default value is `auto`. `auto` means `anthropic` if the variable in `llm.api_key_env` is set, and `none` if it is not set.
- **Why:** One default works for users with a key and for users without a key. An explicit value always wins.

## D5. Price keys for the budget guard

- **Context:** `llm.max_usd_per_change` needs a cost estimate. Prices change, and xis must not hardcode a model.
- **Choice:** Two config keys, `llm.price_input_per_mtok` and `llm.price_output_per_mtok`, with defaults of 3 and 15 USD per million tokens.
- **Why:** The user can set the prices for the model that they use.

## D6. `xis config set` removes comments

- **Context:** The TOML library cannot keep comments when it writes a file.
- **Choice:** `xis config set` rewrites the file without comments and says so in its help text.
- **Why:** A comment-preserving TOML editor is a new dependency for a small benefit.

## D7. Package `internal/xlog` and `internal/buildinfo`

- **Context:** Section 4 has no package for logging or version data.
- **Choice:** Two small packages: `internal/xlog` (the rotating log file) and `internal/buildinfo` (version, commit, date).
- **Why:** Many packages need them. A shared package prevents import cycles.

## D8. Hook events in the database

- **Context:** Ingest must delete the spool file after success. A session can continue after ingest, for example after a `Stop` event or with `claude --resume`. A second ingest then sees only the new spool events.
- **Choice:** Migration `0002` adds the table `hook_events`. Ingest stores each redacted hook event there. A later ingest merges the stored events with the new spool events and builds the session again.
- **Why:** Ingest stays idempotent, and xis keeps no raw (unredacted) data after ingest.

## D9. Suppressions by fingerprint

- **Context:** xis computes the flags of a change again when the change changes. New flag rows get new IDs.
- **Choice:** Migration `0002` adds the table `suppressions` and the column `flags.fingerprint`. `xis flags suppress` stores the fingerprint of the flag. A new flag with the same fingerprint is suppressed.
- **Why:** A suppression must survive a new computation of the flags. The reason must stay visible in reports.

## D10. Unattributed lines in `hunk_provenance`

- **Context:** A hunk can contain lines that no captured step wrote.
- **Choice:** The provenance row with step ID `unattributed` counts these lines.
- **Why:** The contributor of a hunk (`ai`, `human`, `mixed` or `unknown`) needs this count. No new table is necessary.

## D11. Snapshot commit message

- **Context:** Section 7 asks for the message `xis step <step_id>`. The hook does not know the step ID, because ingest makes the step IDs later.
- **Choice:** The message is `xis step <event> <tool_use_id>`. Ingest maps each snapshot to its step through the spool line.
- **Why:** The hook must stay fast and must not need the database.

## D12. Session lock in the hook

- **Context:** Hooks of one session can run at the same time (for example in subagents). Two snapshots at the same time can break the chain.
- **Choice:** The hook takes an exclusive `flock` lock for each session for the snapshot and the spool write. The wait is 8 seconds at most. After that, the hook continues without the lock and logs a warning.
- **Why:** The order of the spool lines is then the order of the chain. The hook never blocks the agent for long.

## D13. Git 2.31 or later

- **Context:** xis uses `git rev-parse --path-format=absolute`.
- **Choice:** xis needs git 2.31 or later. `xis doctor` checks the version.
- **Why:** Absolute paths from git remove a class of path bugs with worktrees. Git 2.31 is from March 2021.

## D14. Seed of the private index

- **Context:** The first `git add -A` with an empty private index hashes every file.
- **Choice:** Before the first snapshot of a session, xis copies the index of the user to the private index. It only reads the index of the user.
- **Why:** Git can then use the file stat data of the user index. The first snapshot gets as fast as the others.

## D15. Shadow commits that point to user commits

- **Context:** The first snapshot has the `HEAD` commit of the user as parent. The commit object is in the user repository. If the user rewrites history and runs `git gc`, git can delete that commit.
- **Choice:** xis accepts this risk. Attribution of a session whose root commit is missing fails with a clear error, and xis treats its hunks as unattributed.
- **Why:** A copy of the user history in the shadow repository costs disk space for a rare case.

## D16. Fixture scrub script

- **Context:** Section 6.4 asks for scrubbed real fixtures.
- **Choice:** `testdata/fixtures/scrub.py` removes paths, user names, signatures and request metadata from transcripts and hook payloads.
- **Why:** The script makes the scrub repeatable. It is a development tool only, not a runtime dependency.

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
- **Note:** The copy keeps the modification time of the user index. Git uses that time to find "racily clean" entries. A newer time made git miss a same-size edit in the same second. The end-to-end test found this bug.

## D15. Shadow commits that point to user commits

- **Context:** The first snapshot has the `HEAD` commit of the user as parent. The commit object is in the user repository. If the user rewrites history and runs `git gc`, git can delete that commit.
- **Choice:** xis accepts this risk. Attribution of a session whose root commit is missing fails with a clear error, and xis treats its hunks as unattributed.
- **Why:** A copy of the user history in the shadow repository costs disk space for a rare case.

## D16. Fixture scrub script

- **Context:** Section 6.4 asks for scrubbed real fixtures.
- **Choice:** `testdata/fixtures/scrub.py` removes paths, user names, signatures and request metadata from transcripts and hook payloads.
- **Why:** The script makes the scrub repeatable. It is a development tool only, not a runtime dependency.

## D17. Session status tags

- **Context:** Section 6.3 asks xis to lower the fidelity when the transcript is missing. The `fidelity` column allows only `live` and `imported`.
- **Choice:** `sessions.status` is a comma-separated list: `active` or `ended`, and `no_transcript` when ingest had no transcript.
- **Why:** The capture is still live, so `fidelity` stays `live`. The UI shows the tag, so the user sees that agent text is missing.

## D18. Exit code of a successful Bash call

- **Context:** Claude Code gives no exit code in `PostToolUse` for `Bash`. A non-zero exit causes `PostToolUseFailure`.
- **Choice:** xis records exit code 0 for a `Bash` call with `PostToolUse` that was not interrupted. The test outcome rules then refuse `pass` if the command can hide the exit code.
- **Why:** This is the observed behavior of Claude Code 2.1.304. Without it, no Claude Code test run could be `pass`.

## D19. Sessions of a range

- **Context:** Section 10 says that a change can cover the sessions "whose snapshot windows overlap the range".
- **Choice:** A session belongs to a `--range` or `--pr` change if it started on the base commit or on a commit in the range. For `--staged`, a session belongs to the change if it started on `HEAD`.
- **Why:** The rule is simple and deterministic. It uses only `sessions.base_commit`.

## D20. Attribution commit

- **Context:** Blame needs one commit that contains the target and the history of all sessions of the change.
- **Choice:** xis makes a commit in the shadow repository with the target tree. Its parents are the final snapshots of the sessions. Git blame follows all parents.
- **Why:** One blame run per file attributes lines to all sessions. No change to the user repository is necessary.

## D21. Removed lines

- **Context:** Section 10 suggests `git log -p --reverse` to find the step that removed a line.
- **Choice:** xis uses the stored edit patches of the session in chain order. The first edit that removed a line with the same text gets the line.
- **Why:** The edits are already in the database. The result is the same, and it needs no git call.

## D22. Content hash input

- **Context:** The Agent Trace specification (v0.1.0) shows `murmur3:` hashes in its examples, but it does not define the input. The reference code was not reachable.
- **Choice:** 32-bit MurmurHash3, seed 0, over the added lines. Each line has collapsed whitespace and no leading or trailing space. xis joins the lines with `\n`. The output is `murmur3:` and 8 lowercase hex digits.
- **Why:** This follows the examples of the specification. If the specification defines the input later, only `attribution.ContentHash` changes.

## D23. Edit blobs

- **Context:** `edits.before_blob` and `edits.after_blob` can hold a blob reference.
- **Choice:** They hold the git blob IDs in the shadow repository. The `blobs` table stays empty in v1.
- **Why:** The shadow repository has the content already. A copy in SQLite would duplicate it and would need its own redaction.

## D24. Scope of the repository root

- **Context:** Section 11 says that a file in a subdirectory of a scope directory is in scope. A read of a root file (for example `go.mod`) puts the root directory in scope.
- **Choice:** The root directory puts only the other root files in scope, not the subdirectories.
- **Why:** Otherwise one read of `README.md` turns off `OUT_OF_SCOPE` for the whole repository.

## D25. `UNATTRIBUTED_CHANGE` needs a fully unattributed hunk

- **Context:** A hunk can have lines from steps and unattributed lines.
- **Choice:** The rule fires only if no captured step wrote any line of the hunk, as the specification says ("no provenance"). The contributor `mixed` shows partial cases.
- **Why:** Uncommitted lines from before the session are common. A flag for each mixed hunk is noise.

## D26. Chain of the git pre-commit hook

- **Context:** `xis precommit install` must chain with an existing hook and never overwrite it.
- **Choice:** xis renames the existing hook to `<name>.xis-orig`. The xis hook runs it first and stops if it fails. Uninstall restores it. An xis error (exit code 2) does not stop the commit.
- **Why:** The existing hook keeps its content and its interpreter. A broken xis must not block all commits.

## D27. Session argument of `xis flags`

- **Context:** Section 14 shows `xis flags [change opts]`.
- **Choice:** `xis flags` also takes an optional `SESSION` argument, as `xis review` does.
- **Why:** The same change options for all commands are easier to learn.

## D28. Target of `LARGE_CHANGE`

- **Context:** The `flags.target_kind` values are `hunk`, `step`, `session` and `file`. `LARGE_CHANGE` is about the whole change.
- **Choice:** The target is the first session of the change.
- **Why:** No schema change is necessary. Reports show the flag at the change level.

## D29. Terminal query of bubbletea

- **Context:** bubbletea v1 asks the terminal for its background color in its package `init`. A terminal that does not answer makes every xis command wait up to 5 seconds. Such a terminal can also lose the first key presses.
- **Choice:** xis accepts this behavior of bubbletea v1. Real terminals answer in a few milliseconds. termenv skips the query for `TERM=dumb`, `screen*` and `tmux*`.
- **Why:** A fork of bubbletea is a large cost. bubbletea v2 removes the query. The hooks are not affected, because their stdout is not a terminal.

## D30. Prompts in place of intent items

- **Context:** Intent items come from the LLM (Section 12). Without an LLM, the UNITS pane has no intent items.
- **Choice:** Without intent items, the pane shows the user prompts of the session under the title PROMPTS.
- **Why:** The prompts are the deterministic source of the intent. The pane never shows an invented status.

## D31. No temperature for Anthropic models

- **Context:** Section 12.2 asks for temperature 0. Current Claude models (for example Claude Sonnet 5.5) reject a non-default `temperature` with a 400 error.
- **Choice:** The Anthropic provider sends no `temperature`. The OpenAI-compatible provider sends `temperature: 0`. Repeatable output comes from the response cache.
- **Why:** A request that the API rejects gives no analysis at all.

## D32. Refusals

- **Context:** A model can decline a request (`stop_reason: refusal`). The API has a server-side fallback feature for some models.
- **Choice:** xis reports a refusal as an error of the task. It does not use the fallback feature. The other tasks continue.
- **Why:** `llm.model` is free. The fallback feature works only with some models and needs a beta header. A failed task never changes the deterministic results.

## D33. Short aliases in prompts

- **Context:** ULIDs are 26 characters. An LLM can copy them wrong.
- **Choice:** The prompts use aliases such as `s12` and `h3`. The validator maps them to the real IDs.
- **Why:** Short IDs cost fewer tokens. An alias that the LLM invents does not map to any ID, so the validator removes its citation.

## D34. Agent claims only from the same actor

- **Context:** Section 12.3 allows a claim from an assistant or reasoning step. The step must be a provenance step of the hunk or one of the 3 steps before it.
- **Choice:** The claim step must belong to the same agent actor as the provenance step. A hunk from a `human_edit` step has no agent claim.
- **Why:** The end-to-end test showed an agent message quoted as the claim of a human edit. That is wrong attribution.

## D35. Explanations per review unit

- **Context:** Section 12.3 asks for a rationale per hunk.
- **Choice:** xis sends the hunks of one review unit together, at most 6 hunks per call, with the context steps of each hunk. The result is still one rationale per hunk.
- **Why:** The hunks of a unit share context. One call for each hunk costs more and gives the same result.

## D36. LLM grouping after a new computation

- **Context:** `xis review` and `xis flags` compute the change again, with the fixed grouping.
- **Choice:** If the stored units of the change came from the LLM and the change has exactly the same hunks, xis keeps the LLM units. Otherwise the fixed grouping replaces them.
- **Why:** The LLM grouping is valid only for the hunks that it saw.

## D37. Review counts in the note

- **Context:** The note of a commit has the number of review units and the number of understood units. You review the working tree before the commit exists, so the units of the commit change have no status.
- **Choice:** xis takes the counts from the latest working-tree or index change of the same sessions. If there is none, it uses the units of the commit change.
- **Why:** The counts then show the review that you did.

## D38. Canonical JSON for the signature

- **Context:** Section 17 asks for an ed25519 signature "over canonical JSON without sig".
- **Choice:** Canonical JSON is the note without `sig`, with sorted keys, no extra whitespace and numbers as written.
- **Why:** Tools can format the note again. The signature must stay valid unless a value changes.

## D39. Agent Trace mapping

- **Context:** The Agent Trace v0.1.0 specification has files, conversations and ranges. It does not say how to split lines into conversations.
- **Choice:** Each file has one conversation for the AI lines of each session, one for human lines and one for unknown lines. An AI conversation has `model_id` as `provider/model`. A range is a run of consecutive added lines with the same source. The `version` field is `0.1`, because the schema needs `major.minor`. For the working tree, the record has no `vcs` object.
- **Why:** The mapping uses only the specification fields. xis-specific data goes in `metadata` under the key `dev.xis`.

## D40. Synthetic Codex fixture

- **Context:** Section 9.3 asks to build the Codex parser from real rollout files. Codex CLI is not installed in the build environment.
- **Choice:** The fixture is synthetic. It follows the `ResponseItem` definitions in the Codex source and the community descriptions of the format. The parser accepts the known output shapes of several Codex versions and warns on unknown line types.
- **Why:** Section 9.3 allows this path. A real, scrubbed rollout can replace the fixture without code changes.

## D41. Content match for imported sessions

- **Context:** Imported sessions have no snapshots, so blame cannot find a step.
- **Choice:** A hunk line belongs to the last `apply_patch` edit of the session that added a line with the same normalized text. No other fallback applies to imported sessions.
- **Why:** Section 9.3 says that hunks that only shell commands could have made stay unattributed and are never guessed.

## D42. Shared command extraction

- **Context:** Claude Code puts a shell command in a string field. Codex puts it in an array such as `["bash", "-lc", "go test ./..."]`.
- **Choice:** `internal/adapters.Command` reads all known forms. Test detection, the rules and the LLM evidence pack use it.
- **Why:** The rules must see the same command for every agent.

## D43. Homebrew cask instead of a formula

- **Context:** Section 9 asks for a Homebrew tap formula. GoReleaser v2 deprecates `brews` (formulas) and replaces them with `homebrew_casks`.
- **Choice:** `.goreleaser.yaml` publishes a cask to the tap `aaxis-em/homebrew-tap`, with the shell completions. `brew install aaxis-em/tap/xis` installs it.
- **Why:** `goreleaser check` fails on deprecated keys. The cask installs the same binary.

## D44. Release parallelism

- **Context:** A GoReleaser build of all 5 targets in parallel ran out of memory on a machine with 7 GB.
- **Choice:** `make snapshot` runs GoReleaser with `-p 2`.
- **Why:** The build then needs less memory. It takes about 30 seconds more.

## D45. Configured keys in the review UI

- **Context:** Section 18 asks for keymap overrides in `[ui]`.
- **Choice:** `ui.keymap` maps an action name to an extra key. The default keys stay.
- **Why:** The footer and the help text stay correct, and an override cannot remove a key by mistake.

## D46. Redaction of stored hook events

- **Context:** The entropy check of the redactor treats a `tool_use_id` such as `toolu_015dd34kGCsGJ4CBb9vHM2y7` as a secret. The first version redacted whole stored hook events. A second ingest of the same session then could not join the stored events with the transcript, and tool calls appeared twice.
- **Choice:** xis redacts only the content fields of a stored hook event: `tool_input`, `tool_response`, `prompt`, `last_assistant_message`, `error` and `message`. IDs and paths stay unchanged.
- **Why:** IDs are not secrets, and the join needs them. A real Claude Code session in the quickstart test found this bug.

## D47. Repository of a session on a later ingest

- **Context:** Only spool lines with a snapshot carry the repository. After an ingest at `Stop`, the spool can hold only `Stop` and `SessionEnd` lines.
- **Choice:** If the spool has no repository line, ingest takes the repository of the stored session.
- **Why:** Otherwise the second ingest finds no shadow repository and loses all edits. The test `TestE2EIngestTwice` covers this case.

## D48. `checkptr` in race tests

- **Context:** `github.com/spaolacci/murmur3` v1.1.0 uses pointer arithmetic that the Go `checkptr` check rejects. `go test -race` turns on `checkptr`, so the hash tests crash there.
- **Choice:** `make race` and CI run `go test -race -gcflags=all=-d=checkptr=0`. The race detector stays on.
- **Why:** Section 3 names this library for Agent Trace compatibility. The check fails on a pattern in the library, not on a data race. xis code has no unsafe pointer use.

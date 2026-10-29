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
- **Choice:** The hook takes an exclusive `flock` lock for each session for the snapshot and the spool write. The wait is 3 seconds at most (8 seconds before D68). After that, the hook continues without the lock and logs a warning.
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
- **Choice:** Replaced by D60. xis first accepted this behavior of bubbletea v1. Real terminals answer in a few milliseconds. termenv skips the query for `TERM=dumb`, `screen*` and `tmux*`.
- **Why:** A fork of bubbletea is a large cost. bubbletea v2 removes the query. The hooks are not affected, because their stdout is not a terminal.

## D30. Prompts in place of intent items

- **Context:** Intent items come from the LLM (Section 12). Without an LLM, the UNITS pane has no intent items.
- **Choice:** Without intent items, the pane shows the user prompts of the session under the title PROMPTS.
- **Why:** The prompts are the deterministic source of the intent. The pane never shows an invented status.

## D31. No temperature for Anthropic models

- **Context:** Section 12.2 asks for temperature 0. Current Claude models (for example Claude Sonnet 5.5) reject a non-default `temperature` with a 400 error.
- **Choice:** The Anthropic provider sends no `temperature`. The OpenAI-compatible provider sends `temperature: 0`, and drops it if the server rejects it (D51). Repeatable output comes from the response cache.
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

## D49. Tool paths under a symbolic link

- **Context:** git gives the repository top with all symbolic links resolved. The agent can give the same file through a symbolic link. On macOS, `/var` is a link to `/private/var`, so the paths of a repository in a temporary directory do not agree.
- **Choice:** If a tool path is not under the repository top, the rules resolve the links of the longest existing part of the path. Then they compare again.
- **Why:** Otherwise `OUT_OF_SCOPE` flags files in the repository. The path of a deleted file must also map, so xis cannot resolve only the full path. The test `TestRelPathThroughSymlink` covers this case.

## D50. Timing test under the race detector

- **Context:** `TestLargeChangeIsFast` checks the 2,000-hunk budget with a wall clock. The race detector makes code 5 to 10 times slower, and CI runners are slow.
- **Choice:** The test skips itself when the race detector is on. `make test` still runs it.
- **Why:** Under the race detector, the test measures the detector and not xis.

## D51. Request fields that a server rejects

- **Context:** OpenAI-compatible servers do not accept the same fields. Some reject a strict JSON schema or each `response_format`. Some reasoning models reject `temperature: 0` and `max_tokens`. Free tiers often send HTTP 429.
- **Choice:** After HTTP 400 or 422, xis reads the error message. If the message names a field, xis changes that field and sends the request again. The JSON format goes from a strict schema to a schema, to JSON mode, to no format. Without a schema format, the schema goes into the system prompt. After HTTP 429 or 5xx, xis waits and tries again, up to 3 times. The provider keeps the changes for its next requests.
- **Why:** One fixed request shape fails on many servers, and the user cannot fix that in the config. xis checks the JSON of each reply and asks again once, so a weaker format still gives valid output. xis does not change a request for other errors, so a real error stays visible.

## D52. Short form of a stable ID

- **Context:** xis makes the IDs of changes, hunks and flags with `StableID` and a zero time. The first 10 characters of a ULID are the time, so each short form was `0000000000`. A real session showed this. `xis flags suppress ID` could not select a flag.
- **Choice:** The short form drops the zero time prefix. `xis flags suppress` accepts the short form and the full ID.
- **Why:** The other characters come from a hash, so they are different for each ID.

## D53. Alphabet strings in the entropy check

- **Context:** A real session added `alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"`. The entropy check found a secret, and `SECRET_IN_CHANGE` stopped the commit.
- **Choice:** The entropy check ignores a token if at least half of its characters follow the previous character in ASCII order.
- **Why:** A random secret almost never has long ascending runs. An alphabet string always has them.

## D54. Scope of each turn

- **Context:** A real session showed three problems of `OUT_OF_SCOPE`. The agent read files with `cat` in Bash, not with the Read tool. A prompt named only an identifier. A later prompt named a path, and the files of the earlier turn then became out of scope.
- **Choice:** xis infers a scope for each turn, and the scope grows over the session. Paths in the shell commands before the first edit of a turn count as reads. A file whose added lines use a prompt identifier is in scope. If the scope has only identifiers and no file of the turn matches, the rule does not fire for the turn. Dependency files, CI config and build config never get the flag.
- **Why:** Each of these cases flagged every file of a normal change. A rule that flags each file teaches the user to ignore it. Each manifest file already has its own rule.

## D55. Moved lines in REVERT_REAPPLY

- **Context:** An agent that rewrites a whole file makes edits that remove a line and add the same line again. The rule counted these lines as a revert.
- **Choice:** A line that one edit both removes and adds is a moved line. The rule ignores it.
- **Why:** A move is not a sign of uncertainty. The rule must find add, remove and add again over separate edits.

## D56. Blame of copied lines

- **Context:** Section 10 asks for `git blame -M`. `-M` also follows a line that a step copies in a file. A real session added `if !ok { http.NotFound(w, r); return }`, which was already in the file. Blame gave the new lines to the commit before the session, so they became unattributed.
- **Choice:** xis runs blame with `-M` first. For an added line whose blamed commit is not a step, xis runs blame without `-M` and uses that result.
- **Why:** An added line in the change is new in the session range. Its origin must be a step. `-M` stays for lines that a step moved.

## D57. Edits before the session start

- **Context:** A user edit that is in the working tree before the first prompt is not between two turns. Section 9.1 makes a `human_edit` step only between turns.
- **Choice:** Replaced by D61. The first choice was to not attribute such lines. They were unattributed, and `UNATTRIBUTED_CHANGE` reported them.
- **Why:** Before the first snapshot, xis did not observe the edit. Another tool or an earlier agent could have made it. Section 9.3 also marks hunks without proof as unattributed and never guesses.

## D58. Line of a hunk flag

- **Context:** A hunk flag showed the first line of the hunk. In a real session, the location of a `t.Skip` call was 3 lines too early.
- **Choice:** A flag has a line. `TEST_SKIPPED`, `LINT_SUPPRESSED` and `SECRET_IN_CHANGE` set it to the added line that matches. Other hunk flags keep line 0 and show the first line of the hunk. Migration `0003` adds the column. The ID and the fingerprint of a flag do not include the line.
- **Why:** The reviewer goes to the location directly. A suppression must not stop to match when lines above the flag move.

## D59. Default session

- **Context:** A Claude Code session that a user opens and closes again has no prompt and no steps. It was the latest session, so `xis review` showed "none recorded" under PROMPTS. Its flags were the uncommitted work of other sessions.
- **Choice:** Without a session argument, xis selects the latest session that has a prompt. If no session has a prompt, xis selects the latest session. D66 changes "latest" to the latest activity.
- **Why:** The user wants to review the last real work. An empty session has nothing to review.

## D60. No terminal query for the background color

- **Context:** The package `init` of bubbletea v1 asks the terminal for its background color. A terminal that does not answer made every xis command wait 5 seconds, `xis version` too. The review UI did not use the answer: it is dark unless `ui.theme = "light"`.
- **Choice:** The package `internal/termbg` sets the background before the `init` of bubbletea. Since Go 1.21, Go initializes the ready packages in import path order, and `github.com/aaxis-em/...` comes before `github.com/charmbracelet/...`. `auto` reads `COLORFGBG`, else it uses `dark`. The Markdown renderer uses a fixed style, not a terminal query.
- **Why:** The test used a terminal that does not answer. `xis version` went from 5.05 seconds to 0.04 seconds. The first frame of `xis review` went from 5.18 to 0.11 seconds.

## D61. Edits before the session start (human)

- **Context:** D57 left a user edit from before the first prompt unattributed. The user asked to credit it to the human. The risk is the first snapshot: it also holds the uncommitted lines of an earlier agent session.
- **Choice:** The first snapshot is the `human_edit` step "Files changed before the session started". Its edits come from a diff with the final snapshot of the previous session if `HEAD` did not change since then, else with `HEAD`. If the previous session ran when this session started, the step gets no edits. Blame can give a line to this step. The line stays human only if the edit of the step added a line with the same text.
- **Why:** Lines of an earlier agent session must never become human lines. The content check finds them: they are not in the diff after the previous session. The test `TestE2EEditsBeforeTheSession` covers both cases.

## D62. Command of a test run

- **Context:** An agent often runs tests in a compound command, for example a heredoc script followed by `go test`. The report showed the start of the full command.
- **Choice:** A test run keeps the part of the command that matched the test command, for example `go test -race ./...`. The exit code check still reads the full command.
- **Why:** The reviewer wants to know which tests ran.

## D63. Metadata lines in a manifest

- **Context:** A new `go.mod` with only `module` and `go` lines fired `DEPENDENCY_CHANGED` in a real session.
- **Choice:** For a manifest, the rule fires only if a changed line can name a dependency. Each change to a lock file still fires it.
- **Why:** The rule is about new code from outside. A module name or a language version adds none.

## D64. Agent text of another file

- **Context:** In a real session, `xis why` on a `store.go` line quoted an agent note about `main.go` as "agent text near this edit".
- **Choice:** xis leaves out agent text that names another file of the session and not the file of the line. File names match as whole words. Text that names no file stays.
- **Why:** Text about another file misleads the reviewer. Most agent text names no file, so the filter removes little.

## D65. Review by prompt

- **Context:** A session often has many prompts. The user wants to see the change of each prompt, and the total change as the default.
- **Choice:** `p` and `P` cycle from all prompts through each prompt and back. A prompt filter shows the hunks that a step of the turn wrote. A turn is the prompt and the steps up to the next prompt. DIFF shows the added lines of other turns in grey. A jump to a hidden hunk shows all prompts again.
- **Why:** The whole hunk stays visible, so the lines of a prompt keep their context. Line attribution already exists, so the filter needs no new data.

## D66. Default session by activity

- **Context:** A user worked in a long session that started the day before. A second Claude Code window started later and closed. `xis review` showed the second window, and the latest prompts of the user were missing.
- **Choice:** The default session is the session with a prompt and the latest activity. The activity is the end time, or the start time for a session without an end time.
- **Why:** The user reviews the work of the session that they used last, not the session that started last.

## D67. Pasted text in a prompt

- **Context:** Claude Code puts pasted text in `<pasted_content>` tags in the prompt. The PROMPTS list showed the tags in place of the text.
- **Choice:** The adapter removes these tags from the prompt text. It compares the hook prompt and the transcript prompt after the same cleanup.
- **Why:** The tags are not part of the instruction of the user.

## D68. Short spool locks for ingest

- **Context:** A user saw "UserPromptSubmit hook timed out after 10s". The hook needs about 50 milliseconds. But ingest held the session lock for its whole run, and the hook waited up to 8 seconds for it. The transcript of a long session was 12 MB, and other xis commands can hold the database.
- **Choice:** Ingest takes the session lock twice, each time for a short time. It reads the spool, and at the end it removes the bytes that it read. Lines that hooks add in between stay for the next ingest. A separate lock lets only one ingest of a session run at a time, and hooks never take it. The hook waits 3 seconds at most. xis no longer deletes lock files, because another process can hold them.
- **Why:** A hook must finish within the 10 seconds of Claude Code. A deleted lock file lets a second process take a new lock while the first one still holds the old one.

## D69. Commands and data in a shell command

- **Context:** In a long real session, 17 of 19 high flags were false. The command rules searched the full command text, including heredoc bodies and quoted text. A test fixture with `.env.local`, a commit message and a `sed` pattern counted as commands. The message showed the start of the command, not the match.
- **Choice:** The package `shellcmd` removes heredoc bodies, and for the command rules also the text in quotes. A script after `sh -c`, `bash -c`, `eval` or `ssh` stays, because the shell runs it. The message shows the simple command that matched. An `rm` of paths in `/tmp` only is not destructive. `TEST_SKIPPED` reads test files only, because `pending` is a common word in code.
- **Why:** A high flag must point to a real action. Too many false flags teach the user to ignore all flags.

## D70. A git repository in the home directory

- **Context:** A user ran `xis hook install` in `~/test`, which had no repository. The home directory was a repository with no commits. xis took it as the project, and its `.claude/settings.json` is the user settings file. So the hooks ran in every folder. A snapshot of the home directory took more than 10 seconds, and Claude Code stopped the hook.
- **Choice:** `xis hook install` with project scope stops with an error if the repository is the home directory. The hook does not take a snapshot of such a repository. It writes a snapshot error, so the session gets steps without edits.
- **Why:** A snapshot of the home directory is slow, and it would copy all files of the user into the shadow repository.

## D71. Review status of each hunk

- **Context:** A unit can hold many hunks. The user wanted to mark each hunk and see the progress of a unit.
- **Choice:** `u` and `x` mark the current hunk, and `U` marks all hunks of the unit. `u` then goes to the next hunk. The table `hunk_status` keeps the status by a key of the file and the hunk lines, without the line numbers. The unit shows a mark that fills up, and its status follows from its hunks. xis still writes the unit status, for the report and the note. A unit status from an older xis goes to its hunks once.
- **Why:** Hunks get new rows and IDs on each run, so the status needs its own key. A key without line numbers survives edits in other parts of the file, and the session, `--staged` and `--pr` views share it.

## D72. Logo

- **Context:** The repository got a logo in `assets/logo`: a mark, a name, lockups for light and dark backgrounds, and icons.
- **Choice:** The README heading is the lockup. A `picture` element selects the dark or the light lockup by the theme of the reader. The review UI shows `∂ xis` in the accent color `#F5A524`, because a terminal cannot show an image. `∂` has the shape of the mark. `assets/logo/README.md` tells which file to use where.
- **Why:** The README is the first page of the project. The color in the header connects the terminal UI with the logo, and it costs no space.

## D73. Install script and release workflow

- **Context:** The user wanted to install xis with one `curl` command. GoReleaser built the archives, but no workflow published them. The release was a draft, and the Homebrew step failed without its token.
- **Choice:** A tag `v*` starts `.github/workflows/release.yml`, which runs the tests and GoReleaser. GoReleaser publishes the release directly, without a draft. The cask step runs only when `HOMEBREW_TAP_GITHUB_TOKEN` is set. `install.sh` finds the release, downloads the archive for the system, checks its SHA-256 against `checksums.txt` and installs the binary in `~/.local/bin`. CI tests the script on Linux and macOS against a local release server.
- **Why:** A draft release is not the latest release, and a download from it needs a login. The checksum check stops a damaged or changed download. The test runs the real script, so a change of the archive names in GoReleaser fails CI.

## D74. Scrolling lists in the UNITS pane

- **Context:** A real session had 42 prompts. The PROMPTS list did not scroll, so `p` moved the selection out of the pane. The units list scrolled in fixed steps, and it did not show hidden items.
- **Choice:** The units and the prompts are lists that keep the selection visible, in the center when the list scrolls. A dim line `↑ N more` or `↓ N more` replaces the first or the last row when items are hidden. Both lists share the free lines. A list that fits gets all its rows, and the prompts get up to two fifths otherwise.
- **Why:** The selected item must always be on the screen, and the user must see that the list continues.

## D75. Base commit of a session

- **Context:** Claude Code sends `SessionStart` again on resume and after compaction. Each event recorded the current `HEAD`, and ingest took it as the base of the session. A real two-day session got a base from the last hour. Its change lost the earlier work, and most prompts showed 0 hunks.
- **Choice:** The base commit is the parent of the first snapshot of the session. That snapshot never changes. Without it, a stored base stays and a later `HEAD` never replaces it.
- **Why:** The first snapshot has the `HEAD` at the start of capture as its parent, so it is the true base. A re-ingest also repairs a base that an older xis moved.

## D76. Windows support

- **Context:** xis compiled for Windows, but its file locks did nothing there, and nothing ran its tests on Windows. The hook commands used single quotes and backslashes, and many path checks expected forward slashes.
- **Choice:** File locks use `LockFileEx`. The background ingest starts as a detached process. Config and data go to `%APPDATA%\xis` and `%LOCALAPPDATA%\xis`. A hook command has the path of `xis.exe` with forward slashes in double quotes. Git Bash and `cmd.exe` both accept this form. The package `pathx` compares agent paths with git paths. It ignores case, the separator and the Git Bash form `/c/...` on Windows. `doctor` does not check Unix file modes on Windows. `DESTRUCTIVE_COMMAND` also knows `Remove-Item -Recurse -Force`, `rd /s /q` and `del /s /q`. `install.ps1` installs a release from PowerShell. CI runs all tests on Windows and builds `windows/arm64`.
- **Why:** Claude Code runs on Windows with Git for Windows, so xis can capture there. All tests also passed as Windows binaries under Wine with MinGit, before CI ran them on Windows.

## D77. License and third-party notices

- **Context:** The repository was public without a license. The user first chose the PolyForm Noncommercial License, then MIT, and then the Apache License 2.0. The release archives hold the code of more than 100 Go modules, and their licenses require their notices in each copy.
- **Choice:** `LICENSE` is the unchanged Apache License 2.0 text from apache.org. `NOTICE` holds the copyright of xis. Before each release, `scripts/notices.sh` collects the license files of all modules in the binary into `THIRD_PARTY_NOTICES.txt`. The script stops if a module has no license file. The archives hold `LICENSE`, `NOTICE` and that file.
- **Why:** Apache 2.0 keeps xis open source. Its patent grant helps companies to accept it, and section 6 gives no rights to the name or the logo. The notices file meets the conditions of the module licenses.

## D78. No Markdown library in the review UI

- **Context:** The release binary was 47 MB, and a slow connection took minutes for the install. The review UI used glamour for the Markdown of LLM answers. glamour runs `text/template`, and that package calls methods by reflection. Then the Go linker must keep every exported method of every type, for example the full Anthropic SDK.
- **Choice:** A small renderer in `internal/tui` formats the answers: headings, lists, code blocks, bold text and code spans. `scripts/check-size.sh` fails in CI when the binary calls methods by reflection again.
- **Why:** The binary went from 46.7 MB to 26.6 MB, and the download from 14.9 MB to 9.4 MB. Answers are short text with citations, so the small renderer is enough.

## D79. Live capture of Codex sessions

- **Context:** Codex sessions came only from `xis import codex`. Each prompt after an import was missing until the next import, and an import has no snapshots. Codex CLI 0.164 has hooks in the format of Claude Code.
- **Choice:** `xis hook install --agent codex` writes the xis hooks to `.codex/hooks.json`. The hook handler, the spool and the snapshots are the same as for Claude Code. Each spool line has `"agent":"codex"`. Ingest builds the steps from the hook events, then adds the agent text and the exit codes from the rollout. xis does not write the Codex hook approval. The user approves the hooks in Codex once.
- **Why:** The shared code keeps the change small, and the snapshots attribute edits by shell commands too. The approval hash depends on Codex internals, and the approval is a decision of the user.

## D80. User PATH on Windows

- **Context:** `install.ps1` read the user PATH with `[Environment]::GetEnvironmentVariable` and wrote it with `SetEnvironmentVariable`. The first expands entries such as `%JAVA_HOME%\bin`, and the second stores the result as plain text. So each install froze the variables in the PATH of the user.
- **Choice:** The script reads the raw registry value with `DoNotExpandEnvironmentNames` and writes it as `ExpandString`. Then it deletes an unused variable with `SetEnvironmentVariable`, so that Windows tells open programs about the change. The Windows CI job tests a PATH with a `%VAR%` entry.
- **Why:** An installer must not change settings that it does not own.

## D81. Secret rules without the gitleaks library

- **Context:** xis used the gitleaks library only for its default rules. The library brought 50 of the 103 Go modules in the binary, for example archive readers and a config system.
- **Choice:** `internal/redact` embeds `config/gitleaks.toml` of gitleaks v8.30.1 with its MIT license and applies it in about 150 lines. The code copies the gitleaks logic for text. That is the keyword filter, the secret group, the entropy limit and the allowlists. It also covers the `gitleaks:allow` marker and the priority of specific rules over generic rules. Rules that need a file path do not apply to text, as in gitleaks. A comparison with gitleaks on 8,876 texts gave the same 839 findings.
- **Why:** The binary went from 26.6 MB to 23.2 MB, and 50 modules fewer need security updates. To update the rules, copy a newer `gitleaks.toml`.

## D82. Lock files after a session

- **Context:** D68 keeps the lock files of a session, because another process can hold one. Nothing removed them, so the spool directory grew by up to three empty files for each session.
- **Choice:** `xis gc` removes the lock file of a session without a spool file. The file must be older than one day, and xis must be able to take the lock at that moment.
- **Why:** A lock that no process holds and that has no spool file belongs to a finished session.

## D83. No stored answers of `xis ask`

- **Context:** `xis ask` wrote each answer to the table `qa`, but no command read the table.
- **Choice:** xis does not store the answers. The table stays, because SQLite migrations only add. The LLM cache already makes a repeated question free.
- **Why:** Data that nothing reads only grows.

## D84. Live review

- **Context:** The review UI loaded its data once. A new prompt or edit of the agent appeared only after the user closed and opened the UI again.
- **Choice:** Every 2 seconds, the UI checks the spool file and the end time of each session in the change. If one changed, the UI analyzes the change again in the background, which ingests the new events first. Then it swaps the data and keeps the selected hunk, matched by `HunkKey`, and the selected prompt. A review without a session argument keeps the session that it opened.
- **Why:** The checks cost a file stat and a query, so the UI does the expensive analysis only after new events. The hooks stay unchanged.

## D85. Codex code mode and a second ingest

- **Context:** A real Codex 0.164 session showed each command twice. In code mode, the rollout has one `exec` call with a script, and the hooks report the tool that the script calls, with another ID. Also, an ingest without new spool lines took the session as a Claude Code session. So a second ingest of a Codex session failed with "no events".
- **Choice:** Live ingest skips the `exec` calls of the rollout. Without new spool lines, ingest takes the agent of the stored session.
- **Why:** The hooks already give each command with its tool name and input. A second ingest must work for each agent, for example for `xis ingest --session` or a live review.

## D86. The review starts with the story of each prompt

- **Context:** Users found the review screen hard to read. It showed three dense panes at once, and it grouped the hunks by change type and directory. Research on code review gives clear rules. Reviewers first build context (Gonçalves et al., 2025). Related changes must stay together, in a logical order (Baum et al., ICSE 2017). The position of a change has an effect: a defect in the last file has 64% lower odds of detection (Fregnan et al., FSE 2022). Reviewers also anchor on the story of an AI (Tufano et al., ICSE 2025).
- **Choice:** The default view groups the hunks by prompt, file by file in the order of the edits. A story card shows the prompt, the agent reply, and the edits and test runs before any diff. Hunks that no step wrote get the group `? no prompt`. Generated files get a group at the end. The UI shows two panes, and `w` adds the explanation. `space` goes to the next hunk without a mark. `v` shows the review units of older versions.
- **Why:** The prompt is the context that the reviewer needs first. The order of the edits is the order in which the agent thought about the change. The group `? no prompt` puts the hunks that the story does not explain in view, against the anchor effect. One focus pane shows less at once.

## D87. Diffs with line numbers and word marks

- **Context:** The diff showed the code with syntax colors, but only the `+` and `-` marks had the add and delete colors. It had no line numbers, and xis colored each line alone, so a comment over many lines lost its color.
- **Choice:** Each diff line shows the old and the new line number, and a background color over the full line. xis compares a removed line with the added line in the same place, word by word. It marks the changed words with a stronger background. A line that changed in more than 60% of its text gets no word marks. xis highlights the old side and the new side of a hunk as two texts. It paints the tokens itself, because the chroma terminal formatter resets the background after each token.
- **Why:** Line numbers and word marks are the features that make delta and the GitHub diff easy to scan. A rewritten line with word marks is mostly marks, so the marks only add noise.
- **Not done yet:** side-by-side diffs, moved-code detection, a whitespace toggle, a palette for color blindness, and more context lines on request.

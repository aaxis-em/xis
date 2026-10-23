# Contributing

This document tells you how to build and test xis, and how to add an agent adapter or a rule.

## Build and test

1. Install Go 1.26 or later and git 2.31 or later.
2. Run `make build` to build `bin/xis`.
3. Run `make test` to run the tests.
4. Run `make check` before each commit. It runs `gofmt`, `go vet`, `golangci-lint`, the tests and a strict `docs-lint`.

Other targets:

| Target | Purpose |
| --- | --- |
| `make race` | Run the tests with the race detector (needs cgo). |
| `make e2e` | Run the end-to-end tests only. |
| `make bench` | Measure the hook and snapshot latency. |
| `make fixtures` | Check the golden files. |
| `make fixtures-update` | Write the golden files again. |
| `make docs-lint` | Check the documents against the writing rules (warns only). |
| `make snapshot` | Build a local release with GoReleaser. |
| `make install-test` | Test `install.sh` against a local release server. |

The tests need a git identity only inside their own temporary repositories. They set it there.

## Tests

- Each package has unit tests. Each rule has a test that fires and a test that does not fire.
- The adapters, the report and the attribution have golden files in `testdata`. Run `make fixtures-update` only when a change of the output is deliberate. Then read the diff of the golden files.
- The end-to-end tests in `internal/cli` build a repository with `testdata/repos/calc.sh`. They feed hook events through `xis hook claude-code`, change files between the events, and check the exact provenance and flags.
- LLM tests use only the mock provider. They must never need a network.

## Fixtures

The fixtures in `testdata/fixtures/claudecode` are real Claude Code data, scrubbed.

1. Record a session with a debug hook that copies stdin to a file.
2. Run `testdata/fixtures/scrub.py IN OUT --replace OLD=NEW` for each file. Replace all paths, user names and e-mail addresses.
3. Search the result for your user name, your home directory and secrets.
4. Commit the fixture and add a golden test.

> **Caution:** Never commit an unscrubbed transcript. A transcript can contain code, secrets and personal data.

## Add an agent adapter

1. Make a package in `internal/adapters/<agent>`.
2. Parse the agent format into a `model.Trace`. Ignore unknown entry types and fields, and add a warning for each one.
3. Set the fidelity: `live` only if xis captures the session with snapshots, else `imported`.
4. Add a golden test with a scrubbed fixture.
5. Connect the adapter in `internal/ingest` and add a command in `internal/cli`.
6. Describe the observed format and the agent version in `docs/ARCHITECTURE.md`.

## Add a rule

1. Add the rule with `def(ID, severity, check)` in `internal/rules/rules.go`. A rule must use only the `Context`: no network and no LLM.
2. Give each flag a stable fingerprint key, so that a suppression survives a new computation.
3. Add a positive case and a negative case to the table in `internal/rules/rules_test.go`.
4. Add a section to `docs/RULES.md`. Say what the rule detects, why it matters, its false positives and how to tune it. Add a small flowchart if the rule depends on the order of events.

## Documents

Write the documents in Simplified Technical English (ASD-STE100). Use short sentences, active voice and one word for one meaning. Use the terms in [docs/GLOSSARY.md](docs/GLOSSARY.md). Record each decision that the specification does not settle in [docs/DECISIONS.md](docs/DECISIONS.md).

## Release

1. Make sure that `make check` passes on `main`.
2. Push a version tag, for example `git tag v0.1.0 && git push origin v0.1.0`.

The workflow `.github/workflows/release.yml` runs the tests and GoReleaser. GoReleaser publishes the archives for Linux, macOS and Windows and `checksums.txt` to the GitHub release. `install.sh` downloads from that release.

The Homebrew cask is optional. Set the repository secret `HOMEBREW_TAP_GITHUB_TOKEN` to a token that can write to `aaxis-em/homebrew-tap`. Without the secret, the release skips the cask.

The install command works only for a public repository, because it downloads without a login.

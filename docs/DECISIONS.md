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

# Config

xis reads the config in this order. A later source overrides an earlier source.

1. The built-in defaults.
2. The user config file, `~/.config/xis/config.toml`. `$XDG_CONFIG_HOME` changes the directory.
3. The repo config file, `<repo>/.xis.toml`.
4. The environment variables `XIS_<SECTION>_<KEY>`, for example `XIS_LLM_PROVIDER=none`.

In an environment variable, separate list items with a comma. Write map items as `key=value`.

Run `xis config path` to see the file paths. Run `xis config get` to see all values. Run `xis config set KEY VALUE` to change one value.

## `[capture]`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `precise` | bool | `false` | Also take a snapshot before each mutating tool call. Then xis can see changes that happen between steps. |
| `exclude` | list of strings | `.env*`, `*.pem`, `*.key`, `id_rsa*`, `*.p12`, `node_modules/`, `vendor/`, `dist/`, `build/`, `target/`, `.venv/`, `__pycache__/`, `.next/` | Paths that snapshots never include. A pattern without `/` matches in all directories. A pattern that ends in `/` is a directory. |
| `mutating_tools` | list of strings | `Edit`, `Write`, `MultiEdit`, `NotebookEdit`, `Bash`, `mcp__*` | Tools that can change files. A `*` matches any text. |
| `readonly_tools` | list of strings | `Read`, `Grep`, `Glob`, `LS`, `WebFetch`, `WebSearch`, `TodoWrite`, ... | Tools that only read. This list wins over `mutating_tools`. |
| `max_file_bytes` | int (bytes) | `1048576` (1 MB) | Larger files get no line attribution. |
| `max_output_bytes` | int (bytes) | `32768` (32 KB) | xis truncates tool output to this size. |
| `test_commands` | list of regular expressions | `[]` | More shell commands that run tests. |
| `test_globs` | list of globs | `[]` | More test file patterns. |

Example `.xis.toml`:

```toml
[capture]
exclude = [".env*", "secrets/", "*.sqlite"]
test_commands = ["^./scripts/check\\.sh"]
```

## `[rules]`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `disabled` | list of rule IDs | `[]` | Rules that do not run. |
| `severity` | map of rule ID to severity | `{}` | Severity overrides: `info`, `low`, `medium` or `high`. |
| `large_change_lines` | int (lines) | `500` | `LARGE_CHANGE` fires above this number of changed lines. |
| `large_change_files` | int (files) | `20` | `LARGE_CHANGE` fires above this number of files. |
| `retry_loop_runs` | int (runs) | `4` | `LONG_RETRY_LOOP` fires at this number of failed test runs in a row. |

## `[scope]`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `paths` | list of paths | `[]` | Paths that are always in scope for `OUT_OF_SCOPE`. A path is a directory prefix. |

Example `.xis.toml`:

```toml
[rules]
disabled = ["SENSITIVE_PATH"]
severity = { DEPENDENCY_CHANGED = "high" }

[scope]
paths = ["internal/auth/", "docs/"]
```

## `[llm]`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `provider` | string | `auto` | `auto`, `anthropic`, `openaicompat` or `none`. `auto` means `anthropic` if the variable in `api_key_env` is set, else `none`. |
| `model` | string | `claude-sonnet-5-5` | The model name for the provider. |
| `base_url` | string | `""` | The API URL. Necessary for `openaicompat`, for example `http://localhost:11434/v1` for Ollama. |
| `api_key_env` | string | `ANTHROPIC_API_KEY` | The environment variable that holds the API key. |
| `max_usd_per_change` | float (USD) | `0.50` | xis stops the analysis of one change at this estimated cost. 0 turns off the guard. |
| `send_code` | bool | `true` | If false, xis sends only metadata, paths and agent text, never file contents or tool output. |
| `price_input_per_mtok` | float (USD) | `3.0` | The input price per 1 million tokens, for the cost estimate. |
| `price_output_per_mtok` | float (USD) | `15.0` | The output price per 1 million tokens, for the cost estimate. |
| `max_tokens` | int (tokens) | `4096` | The maximum response size when a task does not set one. |

Example for Ollama:

```toml
[llm]
provider = "openaicompat"
base_url = "http://localhost:11434/v1"
model = "qwen3-coder"
api_key_env = ""
max_usd_per_change = 0
```

To turn off the LLM for one command, set `XIS_LLM_PROVIDER=none`.

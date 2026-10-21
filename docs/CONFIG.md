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

### Other OpenAI-compatible servers

The `openaicompat` provider works with each server that has the OpenAI chat completions API. Set `model` to a name from the model list of the server. Set `api_key_env` to the variable that holds your key.

| Server | `base_url` |
| --- | --- |
| OpenAI | `https://api.openai.com/v1` |
| Groq | `https://api.groq.com/openai/v1` |
| Google Gemini | `https://generativelanguage.googleapis.com/v1beta/openai` |
| OpenRouter | `https://openrouter.ai/api/v1` |
| Ollama, on your computer | `http://localhost:11434/v1` |
| LM Studio, on your computer | `http://localhost:1234/v1` |

Example for Groq:

```toml
[llm]
provider = "openaicompat"
base_url = "https://api.groq.com/openai/v1"
model = "MODEL-NAME"
api_key_env = "GROQ_API_KEY"
price_input_per_mtok = 0
price_output_per_mtok = 0
```

The default prices are the prices of Claude. For a different provider, set the two price keys. Otherwise the cost guard uses wrong values. For a free tier, set both keys to 0.

Servers do not all accept the same request fields. xis adapts to the server:

- **JSON format:** xis first asks for a strict JSON schema. If the server rejects it, xis tries a schema that is not strict, then the JSON mode, then no format. Without a schema format, xis puts the schema in the system prompt.
- **Temperature:** xis sends `temperature: 0`. If the server rejects it, xis sends no temperature.
- **Token limit:** if the server asks for `max_completion_tokens` in place of `max_tokens`, xis uses it.
- **Rate limits:** after HTTP 429 or a server error, xis waits and tries again, up to 3 times. It obeys the `Retry-After` header, up to 1 minute.

xis keeps what it learns about a server until the command ends. xis also removes a reasoning block (`<think>`) and text around the JSON object before it reads a reply.

To turn off the LLM for one command, set `XIS_LLM_PROVIDER=none`.

## `[redact]`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `patterns` | list of regular expressions | `[]` | More secrets to remove. If the expression has a group, xis removes only the group. |
| `entropy_threshold` | float (bits per character) | `4.5` | The minimum Shannon entropy of a high-entropy token. 0 turns off the entropy check. |
| `entropy_min_length` | int (characters) | `20` | The minimum length of a token for the entropy check. |

Example:

```toml
[redact]
patterns = ["internal-token-(\\w+)", "corp_[A-Z0-9]{16}"]
```

## `[retention]`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `snapshots_days` | int (days) | `30` | `xis gc` removes the snapshots and the LLM cache files of older sessions. |
| `raw_days` | int (days) | `7` | `xis gc` removes spool files that ingest did not finish in this time. |

## `[notes]`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `auto` | bool | `false` | If true, `xis precommit install` also installs a `post-commit` hook that runs `xis note write`. |

## `[ui]`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `theme` | string | `auto` | `auto`, `dark`, `light` or `none`. `none` turns off colors. `light` uses a syntax style for light terminals. `auto` reads the `COLORFGBG` environment variable, else it uses `dark`. xis never asks the terminal for its colors. |
| `keymap` | map of action to key | `{}` | More keys for the actions of `xis review`. The default keys stay. |

The keymap actions are `down`, `up`, `pane`, `expand`, `understood`, `question`, `timeline`, `flags`, `ask`, `editor`, `help`, `quit` and `search`. The other actions are `next_flag`, `prev_flag`, `top`, `bottom`, `prev_unit`, `next_unit`, `next_prompt` and `prev_prompt`.

Example:

```toml
[ui]
theme = "light"
keymap = { quit = "ctrl+q", next_unit = "ctrl+n" }
```

`NO_COLOR` and `--no-color` also turn off colors.

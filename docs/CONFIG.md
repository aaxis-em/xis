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

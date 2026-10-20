# Privacy

xis is local-first. It sends no telemetry. Data leaves your machine only if you configure a remote LLM provider.

## Data locations

| Data | Location | Mode |
| --- | --- | --- |
| Config | `~/.config/xis/config.toml` | `0600` |
| Database | `~/.local/share/xis/xis.db` | `0600` |
| Log | `~/.local/share/xis/logs/xis.log` (5 files of 5 MB) | `0600` |

xis creates all directories with mode `0700` and all files with mode `0600`.

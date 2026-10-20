# Privacy

xis is local-first. It sends no telemetry. Data leaves your machine only if you configure a remote LLM provider.

## Data locations

| Data | Location | Mode |
| --- | --- | --- |
| Config | `~/.config/xis/config.toml` | `0600` |
| Database | `~/.local/share/xis/xis.db` | `0600` |
| Spool | `~/.local/share/xis/spool/<session>.jsonl` (raw hook events until ingest) | `0600` |
| Snapshots | `~/.local/share/xis/shadow/<repo>.git` | `0700` |
| Signing key | `~/.local/share/xis/keys/ed25519` | `0600` |
| Log | `~/.local/share/xis/logs/xis.log` (5 files of 5 MB) | `0600` |

xis creates all directories with mode `0700` and all files with mode `0600`.

## Log

By default the log contains only event names, session IDs and tool use IDs. It never contains payloads or tool output.

## Snapshots

A snapshot contains the files of the working tree, without the ignored files and without the paths in `capture.exclude`. The default `capture.exclude` removes `.env*`, `*.pem`, `*.key`, `id_rsa*` and `*.p12` files.

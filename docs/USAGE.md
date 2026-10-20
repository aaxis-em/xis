# Usage

This document describes each xis command with examples.

## `xis init`

The command creates the data directories, the config file, the database and the signing key. Run it once.

```console
$ xis init
created /home/me/.config/xis/config.toml
database /home/me/.local/share/xis/xis.db
created signing key in /home/me/.local/share/xis/keys

Next steps:
  1. In your repository, run: xis hook install --agent claude-code
  2. Use Claude Code as usual.
  3. Run: xis review
```

## `xis hook install` and `xis hook uninstall`

The command adds the xis hooks to the Claude Code settings. The default scope is `project` (`.claude/settings.json` in the repository). Use `--scope user` for `~/.claude/settings.json`.

```console
$ xis hook install --agent claude-code
/work/repo/.claude/settings.json:
  added SessionStart hook: /usr/local/bin/xis hook claude-code SessionStart
  ...
$ xis hook install --agent claude-code
/work/repo/.claude/settings.json: no change
```

The command keeps all other settings and hooks. A second run changes nothing. `xis hook uninstall` removes only the xis hooks.

## `xis doctor`

The command checks git, the config, the permissions, the database, the hooks, the spool, the LLM config and the signing key.

```console
$ xis doctor
ok    git            git 2.43.0
ok    config         /home/me/.config/xis/config.toml
ok    permissions    /home/me/.local/share/xis (0700/0600)
ok    database       /home/me/.local/share/xis/xis.db (2 migrations, 3 sessions)
ok    hooks          installed (project)
ok    spool          empty
ok    llm            off (provider none). Rules, why and flags work without it.
ok    signing key    /home/me/.local/share/xis/keys
```

The exit code is 1 if a check fails.

## `xis version`

The command shows the version, the commit and the build date.

```console
$ xis version
xis v0.1.0
commit: 1a2b3c4
built: 2026-10-20T10:00:00Z
go: go1.26.4 linux/amd64
```

## `xis config`

The command shows and changes the config.

```console
$ xis config path
config:      /home/me/.config/xis/config.toml
data:        /home/me/.local/share/xis
$ xis config set llm.provider none
$ xis config get llm.provider
none
```

# Usage

This document describes each xis command with examples.

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

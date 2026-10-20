# Config

xis reads the config in this order. A later source overrides an earlier source.

1. The built-in defaults.
2. The user config file, `~/.config/xis/config.toml`. `$XDG_CONFIG_HOME` changes the directory.
3. The repo config file, `<repo>/.xis.toml`.
4. The environment variables `XIS_<SECTION>_<KEY>`, for example `XIS_LLM_PROVIDER=none`.

In an environment variable, separate list items with a comma. Write map items as `key=value`.

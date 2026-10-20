# Architecture

This document describes the design of xis. It uses the terms in the [glossary](GLOSSARY.md).

## Packages

| Package | Purpose |
| --- | --- |
| `cmd/xis` | The program entry point. |
| `internal/cli` | The cobra commands, flags and output. |
| `internal/config` | The config file, the defaults and the XDG paths. |
| `internal/xlog` | The rotating log file. |
| `internal/buildinfo` | The version, the commit and the build date. |
| `internal/tools/doclint` | The checker for the writing rules of the documents. |

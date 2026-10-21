#!/bin/sh
# Writes the shell completion scripts into completions/ for the release archives.
set -eu
rm -rf completions
mkdir completions
for sh in bash zsh fish; do
	go run ./cmd/xis completion "$sh" > "completions/xis.$sh"
done

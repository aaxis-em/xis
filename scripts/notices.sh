#!/bin/sh
# Writes the licenses of the Go modules in the xis binary to
# notices/THIRD_PARTY_NOTICES.txt, for the release archives.
set -eu
out="${1:-notices/THIRD_PARTY_NOTICES.txt}"
mkdir -p "$(dirname "$out")"
go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' ./cmd/xis | sort -u >"$out.mods"
{
	echo "Third-party software in xis"
	echo
	while read -r path version dir; do
		lic=$(find "$dir" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'LICENCE*' -o -iname 'COPYING*' -o -iname 'NOTICE*' \) | sort)
		if [ -z "$lic" ]; then
			echo "notices: no license file in $path $version" >&2
			exit 1
		fi
		echo "================================================================"
		echo "$path $version"
		echo "================================================================"
		for f in $lic; do
			echo
			cat "$f"
			echo
		done
	done <"$out.mods"
	echo "================================================================"
	echo "github.com/zricethezav/gitleaks/v8 config/gitleaks.toml (secret rules in internal/redact)"
	echo "================================================================"
	echo
	cat internal/redact/gitleaks.LICENSE
} >"$out"
rm -f "$out.mods"

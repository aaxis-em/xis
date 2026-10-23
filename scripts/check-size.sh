#!/bin/sh
# Fails when the xis binary links code that calls methods by reflection
# (text/template does). Then the linker must keep every exported method of
# every type, and the binary grows by about 20 MB (D78).
set -eu
deps="$(mktemp)"
trap 'rm -f "$deps"' EXIT
CGO_ENABLED=0 go build -ldflags=-dumpdep -o /dev/null ./cmd/xis 2>"$deps"
if grep -q '<ReflectMethod> -> reflect.Value.MethodByName' "$deps"; then
	echo "check-size: the binary calls methods by reflection, which keeps all methods:" >&2
	grep '<ReflectMethod> -> reflect.Value.MethodByName' "$deps" | head -5 >&2
	exit 1
fi
echo "check-size: ok"

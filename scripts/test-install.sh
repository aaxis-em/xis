#!/bin/sh
# Tests install.sh against a local server that serves a release the way
# GitHub does: /latest redirects to /tag/<version>, files are under
# /download/<version>/.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
server=""
cleanup() {
	[ -n "$server" ] && kill "$server" 2>/dev/null || true
	rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

grep -q 'name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"' "$root/.goreleaser.yaml" ||
	{ echo "the archive name in .goreleaser.yaml changed; update install.sh and this test" >&2; exit 1; }

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; esac
version=0.0.1
archive="xis_${version}_${os}_${arch}.tar.gz"
release="$tmp/srv/download/v$version"
mkdir -p "$release" "$tmp/stage"

(cd "$root" && CGO_ENABLED=0 go build -ldflags "-X github.com/aaxis-em/xis/internal/buildinfo.Version=$version" -o "$tmp/stage/xis" ./cmd/xis)
cp "$root/README.md" "$tmp/stage/"
tar -czf "$release/$archive" -C "$tmp/stage" xis README.md
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$release" && sha256sum "$archive" >checksums.txt)
else
	(cd "$release" && shasum -a 256 "$archive" >checksums.txt)
fi

cat >"$tmp/server.py" <<'EOF'
import http.server, os, sys
root, version = sys.argv[1], sys.argv[2]
class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **kw):
        super().__init__(*a, directory=root, **kw)
    def do_HEAD(self):
        if self.path == "/latest":
            self.send_response(302)
            self.send_header("Location", "/tag/v" + version)
            self.end_headers()
        elif self.path.startswith("/tag/"):
            self.send_response(200)
            self.end_headers()
        else:
            super().do_HEAD()
    def do_GET(self):
        if self.path == "/latest" or self.path.startswith("/tag/"):
            return self.do_HEAD()
        super().do_GET()
    def log_message(self, *a):
        pass
srv = http.server.HTTPServer(("127.0.0.1", 0), Handler)
print(srv.server_address[1], flush=True)
srv.serve_forever()
EOF
python3 "$tmp/server.py" "$tmp/srv" "$version" >"$tmp/port" &
server=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	[ -s "$tmp/port" ] && break
	sleep 0.2
done
base="http://127.0.0.1:$(cat "$tmp/port")"

check() {
	name="$1"
	shift
	if "$@" >"$tmp/out" 2>&1; then
		echo "ok   $name"
	else
		echo "FAIL $name" >&2
		cat "$tmp/out" >&2
		exit 1
	fi
}

check "latest release" env XIS_BASE_URL="$base" XIS_INSTALL_DIR="$tmp/bin1" sh "$root/install.sh"
check "installed binary runs" sh -c "'$tmp/bin1/xis' version | grep -q 'xis $version'"
check "pinned version without v" env XIS_BASE_URL="$base" sh "$root/install.sh" --version "$version" --dir "$tmp/bin2"
check "pinned version runs" test -x "$tmp/bin2/xis"

printf 'deadbeef  %s\n' "$archive" >"$release/checksums.txt"
check "checksum mismatch fails" sh -c "! XIS_BASE_URL='$base' XIS_INSTALL_DIR='$tmp/bin3' sh '$root/install.sh'"
check "nothing installed on mismatch" test ! -e "$tmp/bin3/xis"
check "unknown version fails" sh -c "! XIS_BASE_URL='$base' XIS_INSTALL_DIR='$tmp/bin4' sh '$root/install.sh' --version v9.9.9"

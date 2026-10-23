#!/bin/sh
# Install xis from its GitHub releases:
#
#   curl -fsSL https://raw.githubusercontent.com/aaxis-em/xis/main/install.sh | sh
#
# Options (also as environment variables):
#   --version VERSION   a release such as v0.1.0 (XIS_VERSION, default: latest)
#   --dir DIR           where to put the binary (XIS_INSTALL_DIR, default: ~/.local/bin)
#
# The script checks the SHA-256 of the download against checksums.txt of the
# release and stops on any mismatch.
set -eu

REPO="aaxis-em/xis"
BASE_URL="${XIS_BASE_URL:-https://github.com/$REPO/releases}"
VERSION="${XIS_VERSION:-latest}"
INSTALL_DIR="${XIS_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
	echo "xis install: $*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version) VERSION="${2:?--version needs a value}"; shift 2 ;;
	--dir) INSTALL_DIR="${2:?--dir needs a value}"; shift 2 ;;
	-h | --help) sed -n '2,12p' "$0" 2>/dev/null || true; exit 0 ;;
	*) fail "unknown option $1" ;;
	esac
done

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "no release for $(uname -s); use: go install github.com/$REPO/cmd/xis@latest" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "no release for the CPU $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$2" "$1"; }
	final_url() { curl -fsSLI -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
	final_url() { wget -q -S --spider "$1" 2>&1 | awk '/^ *[Ll]ocation: /{u=$2} END{print u}'; }
else
	fail "curl or wget is necessary"
fi

if [ "$VERSION" = latest ]; then
	VERSION="$(final_url "$BASE_URL/latest" | sed 's|.*/tag/||' | tr -d '\r')"
	case "$VERSION" in
	v[0-9]*) ;;
	*) fail "no published release found at $BASE_URL" ;;
	esac
fi
case "$VERSION" in
v*) ;;
*) VERSION="v$VERSION" ;;
esac

archive="xis_${VERSION#v}_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading xis $VERSION for $os/$arch"
fetch "$BASE_URL/download/$VERSION/$archive" "$tmp/$archive" || fail "download of $archive failed"
fetch "$BASE_URL/download/$VERSION/checksums.txt" "$tmp/checksums.txt" || fail "download of checksums.txt failed"

want="$(awk -v f="$archive" '$2 == f {print $1}' "$tmp/checksums.txt")"
[ -n "$want" ] || fail "checksums.txt has no entry for $archive"
if command -v sha256sum >/dev/null 2>&1; then
	got="$(sha256sum "$tmp/$archive" | cut -d' ' -f1)"
else
	got="$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)"
fi
[ "$got" = "$want" ] || fail "checksum mismatch for $archive (got $got, want $want)"

tar -xzf "$tmp/$archive" -C "$tmp" xis
mkdir -p "$INSTALL_DIR"
mv "$tmp/xis" "$INSTALL_DIR/xis"
chmod 0755 "$INSTALL_DIR/xis"

echo "Installed $("$INSTALL_DIR/xis" version | head -n 1) to $INSTALL_DIR/xis"
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*) echo "Add $INSTALL_DIR to your PATH, for example: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
echo "Next: run 'xis init' once, then 'xis hook install --agent claude-code' in each repository."

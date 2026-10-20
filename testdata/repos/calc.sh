#!/bin/sh
# Builds the "calc" repository for the end-to-end tests in the directory $1.
set -eu
dir="$1"
mkdir -p "$dir"
cd "$dir"
git init -q -b main
git config user.name test
git config user.email test@example.com
git config commit.gpgsign false
git config gc.auto 0
cat > go.mod <<'X'
module calc

go 1.22
X
cat > calc.go <<'X'
package calc

func Add(a, b int) int {
	return a - b
}
X
cat > util.go <<'X'
package calc

const Version = "1.0"
X
cat > calc_test.go <<'X'
package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("bad")
	}
}
X
cat > README.md <<'X'
# calc
X
git add -A
git commit -qm init

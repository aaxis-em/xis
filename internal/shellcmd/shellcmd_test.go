package shellcmd

import "testing"

func TestWithoutHeredocs(t *testing.T) {
	for in, want := range map[string]string{
		"cat > a.go <<'EOF'\nrm -rf /\nEOF\ngo test ./...":            "cat > a.go <<'EOF'\ngo test ./...",
		"git commit -F - <<EOF\ngit push --force\nEOF":                "git commit -F - <<EOF",
		"cat <<-END\n\trm -rf x\n\tEND\nls":                           "cat <<-END\nls",
		"python3 - <<'E'\nprint(1)\nE\ngo vet ./... && go test ./...": "python3 - <<'E'\ngo vet ./... && go test ./...",
		`grep x <<< "$v"; rm -rf b`:                                   `grep x <<< "$v"; rm -rf b`,
		"echo $((1 << 2)); ls":                                        "echo $((1 << 2)); ls",
	} {
		if got := WithoutHeredocs(in); got != want {
			t.Errorf("WithoutHeredocs(%q) =\n%q\nwant\n%q", in, got, want)
		}
	}
}

func TestCode(t *testing.T) {
	for in, want := range map[string]string{
		`git commit -m "rm -rf old code"`:           `git commit -m ""`,
		`sed -i 's/--amend/x/' a.go`:                `sed -i '' a.go`,
		`rm -rf "$BUILD"`:                           `rm -rf ""`,
		`bash -c 'rm -rf build'`:                    `bash -c 'rm -rf build'`,
		`sh -ec "git reset --hard"`:                 `sh -ec "git reset --hard"`,
		`eval "git push -f"`:                        `eval "git push -f"`,
		`ssh host 'rm -rf /srv/app'`:                `ssh host 'rm -rf /srv/app'`,
		`echo "a \"quoted\" word" && rm -rf x`:      `echo "" && rm -rf x`,
		"cat > f <<'EOF'\n'unclosed\nEOF\nls 'a b'": "cat > f <<''\nls ''",
	} {
		if got := Code(in); got != want {
			t.Errorf("Code(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSegment(t *testing.T) {
	s := "make && rm -rf build; ls | wc -l"
	if got := Segment(s, 10); got != "rm -rf build" {
		t.Errorf("Segment = %q", got)
	}
	if got := Segment("rm -rf x", 0); got != "rm -rf x" {
		t.Errorf("Segment = %q", got)
	}
}

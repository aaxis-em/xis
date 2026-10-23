package pathx

import (
	"runtime"
	"testing"
)

func TestRel(t *testing.T) {
	cases := []struct {
		dir, p, want string
		ok           bool
	}{
		{"/repo", "/repo/a.go", "a.go", true},
		{"/repo", "/repo/sub/./b.go", "sub/b.go", true},
		{"/repo", "/repo", ".", true},
		{"/repo", "/repository/a.go", "", false},
		{"/repo", "/other/a.go", "", false},
		{"/repo/", "/repo/a.go", "a.go", true},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases,
			struct {
				dir, p, want string
				ok           bool
			}{"C:/Users/me/repo", `C:\Users\me\repo\src\a.go`, "src/a.go", true},
			struct {
				dir, p, want string
				ok           bool
			}{"C:/Users/me/repo", `c:\users\ME\repo\a.go`, "a.go", true},
			struct {
				dir, p, want string
				ok           bool
			}{"C:/Users/me/repo", "/c/Users/me/repo/a.go", "a.go", true},
			struct {
				dir, p, want string
				ok           bool
			}{`C:\`, `C:\a.go`, "a.go", true},
		)
	}
	for _, c := range cases {
		got, ok := Rel(c.dir, c.p)
		if got != c.want || ok != c.ok {
			t.Errorf("Rel(%q, %q) = %q, %v; want %q, %v", c.dir, c.p, got, ok, c.want, c.ok)
		}
	}
}

func TestSame(t *testing.T) {
	if !Same("/a/b/", "/a/b") || Same("/a/b", "/a/c") {
		t.Error("Same")
	}
	if runtime.GOOS == "windows" && !Same(`C:\Users\Me`, "c:/users/me") {
		t.Error("Same ignores case and separators on Windows")
	}
}

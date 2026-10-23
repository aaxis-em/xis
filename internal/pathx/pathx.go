// Package pathx compares the file paths that agents, git and the OS report.
// On Windows they mix separators, the case of letters and the Git Bash form
// /c/Users/... of a drive path.
package pathx

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var msysDrive = regexp.MustCompile(`^/([A-Za-z])(/|$)`)

// Slash cleans p and returns it with forward slashes, the form of git paths.
func Slash(p string) string {
	if runtime.GOOS == "windows" {
		p = msysDrive.ReplaceAllString(p, "$1:/")
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// Rel returns p relative to dir, with forward slashes. It returns false when
// p is not dir or inside it.
func Rel(dir, p string) (string, bool) {
	d, q := Slash(dir), Slash(p)
	if equal(d, q) {
		return ".", true
	}
	prefix := strings.TrimSuffix(d, "/") + "/"
	if len(q) > len(prefix) && equal(q[:len(prefix)], prefix) {
		return q[len(prefix):], true
	}
	return "", false
}

// Same reports whether a and b name the same path.
func Same(a, b string) bool { return equal(Slash(a), Slash(b)) }

// equal ignores case on Windows, where the file system does.
func equal(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

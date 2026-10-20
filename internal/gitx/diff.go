package gitx

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/sourcegraph/go-diff/diff"
)

type RawChange struct {
	Status  byte
	OldMode string
	NewMode string
	OldBlob string
	NewBlob string
	OldPath string
	Path    string
}

const nullSHA = "0000000000000000000000000000000000000000"

// ParseRaw parses the output of `git diff --raw -z --no-abbrev`.
func ParseRaw(out []byte) ([]RawChange, error) {
	parts := bytes.Split(out, []byte{0})
	var res []RawChange
	for i := 0; i < len(parts); i++ {
		head := string(parts[i])
		if head == "" {
			continue
		}
		if !strings.HasPrefix(head, ":") {
			return res, fmt.Errorf("raw diff: unexpected %q", head)
		}
		f := strings.Fields(head[1:])
		if len(f) < 5 || i+1 >= len(parts) {
			return res, fmt.Errorf("raw diff: short record %q", head)
		}
		c := RawChange{OldMode: f[0], NewMode: f[1], OldBlob: f[2], NewBlob: f[3], Status: f[4][0]}
		if c.OldBlob == nullSHA {
			c.OldBlob = ""
		}
		if c.NewBlob == nullSHA {
			c.NewBlob = ""
		}
		i++
		c.Path = string(parts[i])
		if c.Status == 'R' || c.Status == 'C' {
			if i+1 >= len(parts) {
				return res, fmt.Errorf("raw diff: missing rename target")
			}
			i++
			c.OldPath, c.Path = c.Path, string(parts[i])
		}
		res = append(res, c)
	}
	return res, nil
}

type FilePatch struct {
	Path     string
	OldPath  string
	Binary   bool
	OldMode  string
	NewMode  string
	Deleted  bool
	Created  bool
	Hunks    []*diff.Hunk
	Text     string
	Added    int
	Removed  int
	Extended []string
}

func cleanName(n string) string {
	n = strings.TrimSpace(n)
	if n == "/dev/null" {
		return ""
	}
	if uq, err := strconv.Unquote(n); err == nil && strings.HasPrefix(n, `"`) {
		n = uq
	}
	if strings.HasPrefix(n, "a/") || strings.HasPrefix(n, "b/") {
		return n[2:]
	}
	return n
}

// ParsePatch parses `git diff` output with go-diff and adds the details that
// go-diff leaves in the extended headers (binary files, modes, renames).
func ParsePatch(out []byte) ([]FilePatch, error) {
	fds, err := diff.ParseMultiFileDiff(out)
	if err != nil {
		return nil, err
	}
	res := make([]FilePatch, 0, len(fds))
	for _, fd := range fds {
		fp := FilePatch{Hunks: fd.Hunks, Extended: fd.Extended}
		oldName, newName := cleanName(fd.OrigName), cleanName(fd.NewName)
		for _, x := range fd.Extended {
			switch {
			case strings.HasPrefix(x, "diff --git "):
				if oldName == "" && newName == "" {
					a, b := splitGitHeader(strings.TrimPrefix(x, "diff --git "))
					oldName, newName = a, b
				}
			case strings.HasPrefix(x, "old mode "):
				fp.OldMode = strings.TrimPrefix(x, "old mode ")
			case strings.HasPrefix(x, "new mode "):
				fp.NewMode = strings.TrimPrefix(x, "new mode ")
			case strings.HasPrefix(x, "deleted file mode "):
				fp.Deleted = true
				fp.OldMode = strings.TrimPrefix(x, "deleted file mode ")
			case strings.HasPrefix(x, "new file mode "):
				fp.Created = true
				fp.NewMode = strings.TrimPrefix(x, "new file mode ")
			case strings.HasPrefix(x, "rename from "):
				oldName = unquote(strings.TrimPrefix(x, "rename from "))
			case strings.HasPrefix(x, "rename to "):
				newName = unquote(strings.TrimPrefix(x, "rename to "))
			case strings.HasPrefix(x, "Binary files ") || x == "GIT binary patch":
				fp.Binary = true
			}
		}
		if fd.NewName == "/dev/null" {
			fp.Deleted = true
		}
		if fd.OrigName == "/dev/null" {
			fp.Created = true
		}
		fp.Path, fp.OldPath = newName, oldName
		if fp.Path == "" {
			fp.Path = oldName
		}
		if fp.OldPath == fp.Path {
			fp.OldPath = ""
		}
		st := fd.Stat()
		fp.Added, fp.Removed = int(st.Added+st.Changed), int(st.Deleted+st.Changed)
		if text, err := diff.PrintFileDiff(fd); err == nil {
			fp.Text = string(text)
		}
		res = append(res, fp)
	}
	return res, nil
}

func unquote(s string) string {
	if uq, err := strconv.Unquote(s); err == nil && strings.HasPrefix(s, `"`) {
		return uq
	}
	return s
}

func splitGitHeader(s string) (string, string) {
	if strings.HasPrefix(s, `"`) {
		if end := strings.Index(s[1:], `" `); end >= 0 {
			return cleanName(s[:end+2]), cleanName(s[end+3:])
		}
	}
	if i := strings.Index(s, " b/"); i >= 0 {
		return cleanName(s[:i]), cleanName(s[i+1:])
	}
	return cleanName(s), cleanName(s)
}

type Line struct {
	Kind    byte
	Text    string
	OldLine int
	NewLine int
}

func HunkLines(h *diff.Hunk) []Line {
	var out []Line
	oldN, newN := int(h.OrigStartLine), int(h.NewStartLine)
	for _, raw := range strings.Split(strings.TrimSuffix(string(h.Body), "\n"), "\n") {
		if raw == "" {
			out = append(out, Line{Kind: ' ', OldLine: oldN, NewLine: newN})
			oldN++
			newN++
			continue
		}
		switch raw[0] {
		case '+':
			out = append(out, Line{Kind: '+', Text: raw[1:], NewLine: newN})
			newN++
		case '-':
			out = append(out, Line{Kind: '-', Text: raw[1:], OldLine: oldN})
			oldN++
		case '\\':
		default:
			out = append(out, Line{Kind: ' ', Text: raw[1:], OldLine: oldN, NewLine: newN})
			oldN++
			newN++
		}
	}
	return out
}

func HunkHeader(h *diff.Hunk) string {
	s := fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OrigStartLine, h.OrigLines, h.NewStartLine, h.NewLines)
	if h.Section != "" {
		s += " " + h.Section
	}
	return s
}

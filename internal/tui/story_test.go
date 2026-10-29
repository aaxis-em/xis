package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/aaxis-em/xis/internal/model"
)

func storyData() *Data {
	d := twoPromptData()
	d.Provenance["h2-1"] = []model.HunkProvenance{{HunkID: "h2-1", StepID: model.Unattributed, Lines: 3}}
	d.Units = append(d.Units, Unit{ReviewUnit: model.ReviewUnit{ID: "gen"}, Hunks: []model.Hunk{{ID: "lock", FilePath: "go.sum", ChangeType: model.ChangeGenerated, NewStart: 1, Patch: "@@ -1 +1 @@\n-a\n+b\n"}}})
	return d
}

func outlineText(m *Model) []string {
	var out []string
	for _, g := range m.groups {
		row := m.groupTitle(g) + ":"
		for _, u := range g.units {
			var ids []string
			for _, h := range m.units[u].Hunks {
				ids = append(ids, h.ID)
			}
			row += fmt.Sprintf(" %s[%s]", m.units[u].Title, strings.Join(ids, ","))
		}
		out = append(out, row)
	}
	return out
}

func TestStoryGroupsHunksByPrompt(t *testing.T) {
	m := New(storyData(), Callbacks{}, false)
	want := []string{
		"#1 Fix the login timeout: src/auth/file0.go[h0-0,h0-1] src/auth/file1.go[h1-0] src/auth/file2.go[h2-0]",
		"#5 Also log each retry: src/auth/file1.go[h1-0,h1-1]",
		"? no prompt: src/auth/file2.go[h2-1]",
		"⋯ 1 generated files: go.sum[lock]",
	}
	if got := outlineText(m); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("outline:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !m.onGroup || m.headGroup != 0 {
		t.Fatalf("start: onGroup %v group %d", m.onGroup, m.headGroup)
	}
	m.width, m.height = 120, 30
	if v := stripANSI(m.View()); !strings.Contains(v, "PROMPT #1") || !strings.Contains(v, "Wrap refresh in a retry") || !strings.Contains(v, "WHAT IT DID") {
		t.Errorf("story card missing:\n%s", v)
	}

	m.Update(key("enter"))
	if h, ok := m.curHunk(); !ok || h.ID != "h0-0" || m.focus != paneDiff {
		t.Errorf("enter: hunk %s focus %d", h.ID, m.focus)
	}
	m.Update(key("u"))
	m.Update(key("p"))
	if g := m.curGroup(); !m.onGroup || m.groups[g].turn != 1 {
		t.Errorf("p: group %d on heading %v", g, m.onGroup)
	}
	// h1-0 is under both prompts; a mark under one shows under the other.
	m.Update(key("enter"))
	m.Update(key("u"))
	m.Update(key("P"))
	m.Update(key("enter"))
	m.Update(key(" "))
	if h, _ := m.curHunk(); h.ID != "h0-1" {
		t.Errorf("space from h0-0: hunk %s, want h0-1", h.ID)
	}
	m.Update(key(" "))
	if h, _ := m.curHunk(); h.ID != "h2-0" {
		t.Errorf("space from h0-1 must skip the marked h1-0: hunk %s, want h2-0", h.ID)
	}
	m.Update(key("v"))
	if m.story || len(m.units) != 4 {
		t.Errorf("v: story %v, %d units", m.story, len(m.units))
	}
	if h, _ := m.curHunk(); h.ID != "h2-0" {
		t.Errorf("v lost the hunk: %s", h.ID)
	}
	m.Update(key("v"))
	if !m.story {
		t.Error("v did not come back to the story")
	}
}

func TestStoryLayouts(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	for _, w := range []int{70, 110, 160} {
		m := New(storyData(), Callbacks{}, true)
		m.width, m.height = w, 24
		for _, k := range []string{"", "enter", "w", "esc", "j", "j", "j", "j", "j", "j"} {
			if k != "" {
				m.Update(key(k))
			}
			lines := strings.Split(m.View(), "\n")
			if len(lines) != m.height {
				t.Errorf("width %d after %q: %d lines", w, k, len(lines))
			}
			for i, l := range lines {
				if n := len([]rune(stripANSI(l))); n > w {
					t.Errorf("width %d after %q: line %d is %d wide", w, k, i, n)
				}
			}
		}
	}
	m := New(storyData(), Callbacks{}, false)
	m.width, m.height = 120, 24
	m.setRow(outlineRow{group: 2, unit: -1})
	if v := stripANSI(m.View()); !strings.Contains(v, "NO PROMPT") || !strings.Contains(v, "? no prompt") {
		t.Errorf("no prompt group:\n%s", v)
	}
}

func TestDiffGutterAndWords(t *testing.T) {
	h := model.Hunk{ID: "x", FilePath: "a.go", OldStart: 9, NewStart: 9, NewLines: 3, Patch: "@@ -9,2 +9,2 @@ func Add() {\n ctx\n-\treturn a - b\n+\treturn a + b\n"}
	if got := hunkRange(h, "@@ -9,2 +9,2 @@ func Add() {"); got != "lines 9–11 · func Add() {" {
		t.Errorf("range = %q", got)
	}
	hl := newHighlighter(false)
	got := hl.render(h, newStyles(false), 30)
	want := []string{" 9  9   ctx", "10    - " + "    return a - b", "   10 +     return a + b"}
	for i := range want {
		if strings.TrimRight(got[i], " ") != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	_, lines := parseHunk(h)
	emph := wordEmphasis(lines)
	mark := func(s string, m []bool) string {
		var b strings.Builder
		for i := range s {
			if m != nil && m[i] {
				b.WriteByte(s[i])
			}
		}
		return b.String()
	}
	if a, b := mark(expandTabs(lines[1].text), emph[1]), mark(expandTabs(lines[2].text), emph[2]); a != "-" || b != "+" {
		t.Errorf("changed words = %q / %q, want - / +", a, b)
	}
	if _, _, ok := changedWords("alpha beta gamma", "one two three"); ok {
		t.Error("a line rewritten in full got word marks")
	}
}

func TestHighlightKeepsCommentAcrossLines(t *testing.T) {
	hl := newHighlighter(true)
	lines := []diffLine{{kind: ' ', text: "/* start"}, {kind: '+', text: "still a comment */"}, {kind: ' ', text: "x := 1"}}
	toks := hl.tokens("a.go", lines)
	if len(toks[1]) == 0 || !strings.Contains(toks[1][0].Type.String(), "Comment") {
		t.Errorf("second line of the comment = %v", toks[1])
	}
}

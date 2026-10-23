package tui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	m := New(testData(1, 1), Callbacks{}, false)
	text := "## Why the retry\n\nThe agent wrapped **Refresh** in a retry [step:s1]. It calls `c.do(req)` up to `cfg.Retries` times, and a long sentence that must wrap at the width.\n\n- first point about the change [hunk:h1]\n- second point that is long enough to wrap onto the next line\n\n1. one\n2. two\n\n```go\nfor i := 0; i < n; i++ {\n```\n"
	got := m.renderMarkdown(text, 40)
	want := []string{
		"Why the retry",
		"",
		"The agent wrapped Refresh in a retry",
		"[step:s1]. It calls c.do(req) up to",
		"cfg.Retries times, and a long sentence",
		"that must wrap at the width.",
		"",
		"• first point about the change [hunk:h1]",
		"• second point that is long enough to",
		"  wrap onto the next line",
		"",
		"1. one",
		"2. two",
		"",
		"  for i := 0; i < n; i++ {",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\n\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, l := range got {
		if len([]rune(l)) > 40 {
			t.Errorf("line longer than the width: %q", l)
		}
	}
}

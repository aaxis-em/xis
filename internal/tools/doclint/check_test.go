package main

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	doc := strings.Join([]string{
		"# Title",
		"",
		"The step is recorded by the hook.",
		"Run `xis init` to utilize the tool.",
		"xis records the step.",
		"1. Run the command and then open the file and then read every line of the output slowly and then close the file and start again.",
		"```",
		"this line is ignored even if it is very very long and it is written in passive voice",
		"```",
		"| a | b is ignored |",
		"> \"quoted agent text is skipped and it is written by the agent\"",
	}, "\n")
	ws := Check(doc)
	var msgs []string
	for _, w := range ws {
		msgs = append(msgs, w.Msg)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{`passive voice "is recorded"`, `"utilize"`, "procedural sentence has"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	for _, w := range ws {
		if w.Line == 5 || w.Line >= 7 {
			t.Errorf("unexpected warning on line %d: %s", w.Line, w.Msg)
		}
	}
}

func TestSplitSentences(t *testing.T) {
	got := splitSentences("Edit config.toml now. Then run it, e.g. twice! Done?")
	if len(got) != 3 {
		t.Fatalf("got %q", got)
	}
}

package tui

import (
	"testing"

	"github.com/aaxis-em/xis/internal/model"
)

func TestRefreshKeepsSelection(t *testing.T) {
	next := twoPromptData()
	calls := 0
	m := New(twoPromptData(), Callbacks{Refresh: func() (*Data, error) {
		calls++
		if calls == 1 {
			return nil, nil
		}
		return next, nil
	}}, false)
	if m.Init() == nil {
		t.Fatal("no refresh tick with a Refresh callback")
	}
	m.Update(key("p"))
	m.Update(key("p"))
	m.focus = paneDiff
	m.Update(key("j"))
	before, _ := m.curHunk()

	_, cmd := m.Update(tickMsg{})
	m.Update(cmd())
	if m.data == next || m.status != "" {
		t.Fatalf("a refresh without changes reloaded: status %q", m.status)
	}

	extra := Unit{ReviewUnit: model.ReviewUnit{ID: "u-new", Title: "New unit"}}
	extra.Hunks = []model.Hunk{{ID: "h-new", FilePath: "src/new.go", Patch: "@@ -1 +1 @@\n+x"}}
	next.Units = append([]Unit{extra}, next.Units...)
	_, cmd = m.Update(tickMsg{})
	m.Update(cmd())
	after, _ := m.curHunk()
	if m.data != next || m.status != "updated from the session" {
		t.Fatalf("not reloaded: status %q", m.status)
	}
	if after.ID != before.ID || m.prompt != 1 {
		t.Errorf("selection lost: hunk %s -> %s, prompt %d", before.ID, after.ID, m.prompt)
	}
	if len(m.all) != len(twoPromptData().hunks())+1 {
		t.Errorf("new hunk missing: %d hunks", len(m.all))
	}
}

func TestNoRefreshWithoutCallback(t *testing.T) {
	m := New(testData(1, 1), Callbacks{}, false)
	if m.Init() != nil {
		t.Error("refresh tick without a Refresh callback")
	}
}

package tui

import (
	"strings"
	"testing"
)

func TestHunkProgress(t *testing.T) {
	var units []string
	hunks := map[string]string{}
	cb := Callbacks{
		SetUnitStatus: func(id, st string) error { units = append(units, id+"="+st); return nil },
		SetHunkStatus: func(k, st string) error { hunks[k] = st; return nil },
	}
	d := testData(2, 4)
	m := byUnit(New(d, cb, false))
	m.width, m.height = 150, 40
	glyph := func() string { return m.progressOf(m.units[0].Hunks).glyph() }

	marks := []string{" "}
	for i := 0; i < 4; i++ {
		m.Update(key("u"))
		marks = append(marks, glyph())
	}
	if got := strings.Join(marks, ""); got != " ◔◑◕✔" {
		t.Errorf("progress marks = %q", got)
	}
	if m.unit != 1 || m.hunk != 0 {
		t.Errorf("u did not move on: unit %d hunk %d", m.unit, m.hunk)
	}
	if strings.Join(units, ",") != "u0=understood" {
		t.Errorf("unit statuses = %v", units)
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "4/8 understood") || !strings.Contains(view, "✔1 Unit 1") {
		t.Errorf("view lacks the progress:\n%s", view)
	}

	m.Update(key("u"))
	m.focus = paneDiff
	m.Update(key("k"))
	if h, _ := m.curHunk(); m.hunkStatus(h) != "understood" {
		t.Fatalf("hunk %s is %s", h.ID, m.hunkStatus(h))
	}
	m.Update(key("u"))
	if h, _ := m.curHunk(); m.hunkStatus(h) != "unreviewed" || hunks[HunkKey(h)] != "unreviewed" {
		t.Errorf("second u did not clear the hunk")
	}
	if !strings.Contains(stripANSI(m.View()), "1/4") {
		t.Error("partial unit shows no count")
	}

	m.Update(key("U"))
	if p := m.progressOf(m.units[1].Hunks); p.status() != "understood" {
		t.Errorf("U: %+v", p)
	}
	m.Update(key("x"))
	if p := m.progressOf(m.units[1].Hunks); p.glyph() != "?" || units[len(units)-1] != "u1=questioned" {
		t.Errorf("x: %+v %v", p, units)
	}

	saved := map[string]string{}
	for k, v := range hunks {
		if v != "unreviewed" {
			saved[k] = v
		}
	}
	d2 := testData(2, 4)
	d2.HunkStatus = saved
	m2 := byUnit(New(d2, Callbacks{}, false))
	if p := m2.progressOf(m2.units[0].Hunks); p.glyph() != "✔" {
		t.Errorf("saved status not shown again: %+v", p)
	}
}

func TestHunkKeyIgnoresPosition(t *testing.T) {
	d := testData(1, 1)
	h := d.Units[0].Hunks[0]
	moved := h
	moved.NewStart, moved.OldStart = 90, 90
	moved.Patch = "@@ -90,2 +90,3 @@" + h.Patch[strings.IndexByte(h.Patch, '\n'):]
	if HunkKey(h) != HunkKey(moved) {
		t.Error("a moved hunk has a new key")
	}
	other := h
	other.FilePath = "other.go"
	if HunkKey(h) == HunkKey(other) {
		t.Error("two files share a key")
	}
}

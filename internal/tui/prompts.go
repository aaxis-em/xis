package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aaxis-em/xis/internal/model"
)

// turn is a prompt and every step up to the next prompt of its session.
type turn struct {
	prompt model.Step
	steps  map[string]bool
	hunks  int
}

func buildTurns(d *Data) []turn {
	sessions := make([]string, 0, len(d.Sessions))
	for id := range d.Sessions {
		sessions = append(sessions, id)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return d.Sessions[sessions[i]].Session.StartedAt.Before(d.Sessions[sessions[j]].Session.StartedAt)
	})
	var out []turn
	for _, id := range sessions {
		cur := -1
		for _, st := range d.Sessions[id].Steps {
			if st.Kind == model.StepUserMessage {
				out = append(out, turn{prompt: st, steps: map[string]bool{}})
				cur = len(out) - 1
			}
			if cur >= 0 {
				out[cur].steps[st.ID] = true
			}
		}
	}
	for i := range out {
		for _, u := range d.Units {
			for _, h := range u.Hunks {
				if out[i].wrote(d, h) {
					out[i].hunks++
				}
			}
		}
	}
	return out
}

func (t turn) wrote(d *Data, h model.Hunk) bool {
	for _, p := range d.Provenance[h.ID] {
		if t.steps[p.StepID] {
			return true
		}
	}
	return false
}

func (m *Model) curTurn() (turn, bool) {
	if m.prompt < 0 || m.prompt >= len(m.turns) {
		return turn{}, false
	}
	return m.turns[m.prompt], true
}

// selectPrompt shows only the hunks that prompt i changed; -1 shows all.
// The keys cycle from all prompts through each prompt and back.
func (m *Model) selectPrompt(i int) {
	switch {
	case i < -1:
		i = len(m.turns) - 1
	case i >= len(m.turns):
		i = -1
	}
	cur, _ := m.curHunk()
	m.prompt = i
	m.applyPrompt()
	m.unit, m.hunk, m.diffScroll, m.whyScroll = 0, 0, 0, 0
	if ui, hi, ok := findHunk(m.units, func(h model.Hunk) bool { return h.ID == cur.ID }); ok {
		m.unit, m.hunk = ui, hi
	}
	if t, ok := m.curTurn(); ok {
		m.status = fmt.Sprintf("prompt #%d: %s", t.prompt.Seq, oneLine(t.prompt.Text, 80))
	} else {
		m.status = "all prompts"
	}
}

func (m *Model) applyPrompt() {
	t, ok := m.curTurn()
	if !ok {
		m.units = m.data.Units
		return
	}
	m.units = nil
	for _, u := range m.data.Units {
		var hs []model.Hunk
		for _, h := range u.Hunks {
			if t.wrote(m.data, h) {
				hs = append(hs, h)
			}
		}
		if len(hs) > 0 {
			u.Hunks = hs
			m.units = append(m.units, u)
		}
	}
}

// dimOtherPrompts greys the added lines of the hunk that another prompt
// wrote, so the lines of the selected prompt stand out in their context.
func (m *Model) dimOtherPrompts(h model.Hunk, body []string) []string {
	t, ok := m.curTurn()
	if !ok {
		return body
	}
	lineSteps := m.data.LineSteps[h.FilePath]
	_, lines := parseHunk(h)
	out := append([]string(nil), body...)
	for i, l := range lines {
		if i < len(out) && l.kind == '+' && !t.steps[lineSteps[l.new]] {
			out[i] = m.st.dim.Render("+" + strings.ReplaceAll(l.text, "\t", "    "))
		}
	}
	return out
}

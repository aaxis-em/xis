package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaxis-em/xis/internal/model"
)

const refreshEvery = 2 * time.Second

type tickMsg struct{}

type refreshMsg struct {
	data *Data
	err  error
}

func (m *Model) Init() tea.Cmd { return m.nextTick() }

func (m *Model) nextTick() tea.Cmd {
	if m.cb.Refresh == nil {
		return nil
	}
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

// refresh asks for new data off the UI goroutine. The next tick starts only
// after the answer, so one refresh runs at a time.
func (m *Model) refresh() tea.Cmd {
	refresh := m.cb.Refresh
	return func() tea.Msg {
		d, err := refresh()
		return refreshMsg{d, err}
	}
}

// Reload swaps in new data and keeps the selected hunk and prompt.
func (m *Model) Reload(d *Data) {
	cur, hasCur := m.curHunk()
	curKey := ""
	if hasCur {
		curKey = m.key(cur)
	}
	promptID := ""
	if t, ok := m.curTurn(); ok {
		promptID = t.prompt.ID
	}
	onGroup := m.onGroup
	m.data, m.keyCache = d, nil
	m.turns, m.all = buildTurns(d), d.hunks()
	m.prompt = -1
	for i, t := range m.turns {
		if t.prompt.ID == promptID && !m.story {
			m.prompt = i
		}
	}
	m.applyPrompt()
	if onGroup {
		for gi, g := range m.groups {
			if g.turn >= 0 && m.turns[g.turn].prompt.ID == promptID {
				m.headGroup = gi
				return
			}
		}
		m.openFirstGroup()
		return
	}
	m.tlSel = min(m.tlSel, max(len(d.Timeline)-1, 0))
	if ui, hi, ok := findHunk(m.units, func(h model.Hunk) bool { return m.key(h) == curKey }); ok {
		m.unit, m.hunk = ui, hi
		return
	}
	m.unit = max(min(m.unit, len(m.units)-1), 0)
	m.hunk, m.diffScroll, m.whyScroll = 0, 0, 0
}

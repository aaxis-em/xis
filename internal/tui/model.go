package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

	"github.com/aaxis-em/xis/internal/model"
)

type viewKind int

const (
	viewMain viewKind = iota
	viewTimeline
	viewStep
	viewFlags
	viewAsk
	viewHelp
)

type pane int

const (
	paneUnits pane = iota
	paneDiff
	paneWhy
)

type citation struct {
	kind, id string
}

type Model struct {
	data  *Data
	cb    Callbacks
	st    styles_
	hl    *highlighter
	color bool

	width, height int
	view          viewKind
	prev          viewKind
	focus         pane

	units  []Unit
	turns  []turn
	prompt int

	unit, hunk int
	diffScroll int
	whyScroll  int

	tlSel, stepScroll int
	stepID            string
	flagSel           int
	flagCursor        int

	input     textinput.Model
	inputMode string
	answer    []string
	citations []citation
	citeSel   int
	asking    bool

	status  string
	keys    map[string]string
	Editor  func(file string, line int) *exec.Cmd
	mdStyle string
}

func New(d *Data, cb Callbacks, color bool) *Model {
	in := textinput.New()
	in.CharLimit = 500
	m := &Model{data: d, cb: cb, st: newStyles(color), hl: newHighlighter(color), color: color, width: 120, height: 40, flagCursor: -1, input: in, mdStyle: "dark", prompt: -1}
	m.turns = buildTurns(d)
	m.applyPrompt()
	m.Editor = defaultEditor
	return m
}

func defaultEditor(file string, line int) *exec.Cmd {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "vi"
	}
	parts := strings.Fields(ed)
	args := append(parts[1:], fmt.Sprintf("+%d", line), file)
	return exec.Command(parts[0], args...)
}

func (m *Model) Init() tea.Cmd { return nil }

type answerMsg struct {
	text string
	err  error
}

type editorDoneMsg struct{ err error }

func (m *Model) layout() int {
	switch {
	case m.width >= 140:
		return 3
	case m.width >= 100:
		return 2
	}
	return 1
}

func (m *Model) curUnit() (Unit, bool) {
	if m.unit < 0 || m.unit >= len(m.units) {
		return Unit{}, false
	}
	return m.units[m.unit], true
}

func (m *Model) curHunk() (model.Hunk, bool) {
	u, ok := m.curUnit()
	if !ok || m.hunk < 0 || m.hunk >= len(u.Hunks) {
		return model.Hunk{}, false
	}
	return u.Hunks[m.hunk], true
}

func (m *Model) selectUnit(i int) {
	if len(m.units) == 0 {
		return
	}
	m.unit = max(0, min(i, len(m.units)-1))
	m.hunk, m.diffScroll, m.whyScroll = 0, 0, 0
}

func (m *Model) selectHunk(i int) {
	u, ok := m.curUnit()
	if !ok || len(u.Hunks) == 0 {
		return
	}
	m.hunk = max(0, min(i, len(u.Hunks)-1))
	m.diffScroll, m.whyScroll = 0, 0
}

func findHunk(units []Unit, match func(model.Hunk) bool) (int, int, bool) {
	for ui, u := range units {
		for hi, h := range u.Hunks {
			if match(h) {
				return ui, hi, true
			}
		}
	}
	return 0, 0, false
}

// locate finds a hunk in the visible units. A hunk that the selected prompt
// did not change brings back all prompts.
func (m *Model) locate(match func(model.Hunk) bool) (int, int, bool) {
	if ui, hi, ok := findHunk(m.units, match); ok {
		return ui, hi, true
	}
	if _, _, ok := findHunk(m.data.Units, match); !ok || m.prompt < 0 {
		return 0, 0, false
	}
	m.prompt = -1
	m.applyPrompt()
	return findHunk(m.units, match)
}

func (m *Model) jumpToHunk(id string) bool {
	if ui, hi, ok := m.locate(func(h model.Hunk) bool { return h.ID == id }); ok {
		m.unit, m.hunk, m.diffScroll, m.whyScroll = ui, hi, 0, 0
		return true
	}
	return false
}

func (m *Model) jumpToFlag(f model.Flag) bool {
	switch f.TargetKind {
	case "hunk":
		return m.jumpToHunk(f.TargetID)
	case "file":
		if ui, hi, ok := m.locate(func(h model.Hunk) bool { return h.FilePath == f.TargetID }); ok {
			m.unit, m.hunk, m.diffScroll = ui, hi, 0
			return true
		}
	case "step":
		if hs := m.hunksOfStep(f.TargetID); len(hs) > 0 {
			return m.jumpToHunk(hs[0])
		}
	}
	return false
}

func (m *Model) hunksOfStep(stepID string) []string {
	var out []string
	for _, u := range m.data.Units {
		for _, h := range u.Hunks {
			for _, p := range m.data.Provenance[h.ID] {
				if p.StepID == stepID {
					out = append(out, h.ID)
				}
			}
		}
	}
	return out
}

func (m *Model) bodyHeight() int { return max(m.height-2, 3) }

func (m *Model) mainHeight() int { return max(m.bodyHeight()-len(m.promptPanel()), 3) }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case answerMsg:
		m.asking = false
		if msg.err != nil {
			m.answer = []string{"Error: " + msg.err.Error()}
			return m, nil
		}
		m.setAnswer(msg.text)
		return m, nil
	case editorDoneMsg:
		if msg.err != nil {
			m.status = "editor: " + msg.err.Error()
		}
		return m, nil
	case tea.KeyMsg:
		if m.inputMode != "" {
			return m.updateInput(msg)
		}
		m.status = ""
		switch m.view {
		case viewMain:
			return m.updateMain(msg)
		case viewTimeline:
			return m.updateTimeline(msg)
		case viewStep:
			return m.updateStep(msg)
		case viewFlags:
			return m.updateFlags(msg)
		case viewAsk:
			return m.updateAsk(msg)
		case viewHelp:
			if k := m.translate(msg.String()); k == "q" || k == "esc" || k == "?" || k == "enter" {
				m.view = m.prev
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) openInput(mode, prompt string) tea.Cmd {
	m.inputMode = mode
	m.input.Prompt = prompt
	m.input.SetValue("")
	return m.input.Focus()
}

func (m *Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.inputMode = ""
		m.input.Blur()
		return m, nil
	case tea.KeyEnter:
		val := strings.TrimSpace(m.input.Value())
		mode := m.inputMode
		m.inputMode = ""
		m.input.Blur()
		switch mode {
		case "search":
			m.search(val)
		case "ask":
			return m, m.ask(val)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) search(q string) {
	if q == "" {
		return
	}
	q = strings.ToLower(q)
	type pos struct{ u, h int }
	var all []pos
	for ui, u := range m.units {
		for hi := range u.Hunks {
			all = append(all, pos{ui, hi})
		}
	}
	start := 0
	for i, p := range all {
		if p.u == m.unit && p.h == m.hunk {
			start = i + 1
		}
	}
	for i := 0; i < len(all); i++ {
		p := all[(start+i)%len(all)]
		h := m.units[p.u].Hunks[p.h]
		if strings.Contains(strings.ToLower(h.FilePath), q) || strings.Contains(strings.ToLower(h.Patch), q) {
			m.unit, m.hunk, m.diffScroll = p.u, p.h, 0
			m.status = fmt.Sprintf("found %q in %s", q, h.FilePath)
			return
		}
	}
	m.status = fmt.Sprintf("%q not found", q)
}

func (m *Model) ask(q string) tea.Cmd {
	if q == "" {
		return nil
	}
	m.view = viewAsk
	m.answer = []string{"> " + q, ""}
	m.citations = nil
	if m.cb.Ask == nil {
		m.answer = append(m.answer, "LLM analysis off. Set llm.provider (or ANTHROPIC_API_KEY) to ask questions. `xis why` works without an LLM.")
		return nil
	}
	m.asking = true
	ask := m.cb.Ask
	return func() tea.Msg {
		text, err := ask(q)
		return answerMsg{text, err}
	}
}

var citeRe = regexp.MustCompile(`\[(step|hunk):([0-9A-Za-z]+)\]`)

func (m *Model) setAnswer(text string) {
	m.citations = nil
	seen := map[string]bool{}
	for _, c := range citeRe.FindAllStringSubmatch(text, -1) {
		if !seen[c[0]] {
			seen[c[0]] = true
			m.citations = append(m.citations, citation{c[1], c[2]})
		}
	}
	rendered := text
	if m.color {
		if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(m.mdStyle), glamour.WithWordWrap(max(m.width-4, 20))); err == nil {
			if out, err := r.Render(text); err == nil {
				rendered = out
			}
		}
	}
	q := m.answer
	if len(q) > 2 {
		q = q[:2]
	}
	m.answer = append(q, strings.Split(strings.TrimRight(rendered, "\n"), "\n")...)
	m.citeSel = 0
}

func (m *Model) setStatus(unitStatus string) {
	u, ok := m.curUnit()
	if !ok {
		return
	}
	if u.Status == unitStatus {
		unitStatus = "unreviewed"
	}
	if m.cb.SetUnitStatus != nil {
		if err := m.cb.SetUnitStatus(u.ID, unitStatus); err != nil {
			m.status = "save status: " + err.Error()
			return
		}
	}
	m.units[m.unit].Status = unitStatus
	for i := range m.data.Units {
		if m.data.Units[i].ID == u.ID {
			m.data.Units[i].Status = unitStatus
		}
	}
	m.status = fmt.Sprintf("unit %d: %s", m.unit+1, unitStatus)
}

func (m *Model) nextFlag(dir int) {
	var targets []int
	for i, f := range m.data.Flags {
		if !f.Suppressed && (f.TargetKind == "hunk" || f.TargetKind == "file" || f.TargetKind == "step") {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		m.status = "no flags"
		return
	}
	pos := -1
	for i, t := range targets {
		if t == m.flagCursor {
			pos = i
		}
	}
	for n := 0; n < len(targets); n++ {
		pos = (pos + dir + len(targets)) % len(targets)
		f := m.data.Flags[targets[pos]]
		if m.jumpToFlag(f) {
			m.flagCursor = targets[pos]
			m.status = fmt.Sprintf("⚑ %s %s: %s", f.Severity, f.RuleID, f.Message)
			return
		}
	}
	m.status = "no flag points to a hunk"
}

func (m *Model) openEditor() tea.Cmd {
	h, ok := m.curHunk()
	if !ok || m.Editor == nil {
		return nil
	}
	file := filepath.Join(m.data.RepoTop, h.FilePath)
	cmd := m.Editor(file, max(h.NewStart, 1))
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{err} })
}

func (m *Model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.translate(msg.String()) {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.focus = (m.focus + 1) % 3
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		switch m.focus {
		case paneUnits:
			m.selectUnit(0)
		case paneDiff:
			m.selectHunk(0)
		default:
			m.whyScroll = 0
		}
	case "G", "end":
		switch m.focus {
		case paneUnits:
			m.selectUnit(len(m.units) - 1)
		case paneDiff:
			u, _ := m.curUnit()
			m.selectHunk(len(u.Hunks) - 1)
		default:
			m.whyScroll = 1 << 20
		}
	case "i":
		if t, ok := m.curTurn(); ok {
			m.stepID, m.stepScroll = t.prompt.ID, 0
			m.prev, m.view = viewMain, viewStep
		}
	case "p":
		m.selectPrompt(m.prompt + 1)
	case "P":
		m.selectPrompt(m.prompt - 1)
	case "[":
		m.selectUnit(m.unit - 1)
	case "]":
		m.selectUnit(m.unit + 1)
	case "enter":
		switch m.focus {
		case paneUnits:
			m.focus = paneDiff
		case paneDiff:
			m.focus = paneWhy
		}
	case "u":
		m.setStatus("understood")
	case "x":
		m.setStatus("questioned")
	case "t":
		m.prev, m.view = viewMain, viewTimeline
	case "f":
		m.prev, m.view = viewMain, viewFlags
	case "a":
		return m, m.openInput("ask", "ask> ")
	case "/":
		return m, m.openInput("search", "/")
	case "n":
		m.nextFlag(1)
	case "N":
		m.nextFlag(-1)
	case "o":
		return m, m.openEditor()
	case "?":
		m.prev, m.view = viewMain, viewHelp
	}
	return m, nil
}

func (m *Model) move(d int) {
	switch m.focus {
	case paneUnits:
		m.selectUnit(m.unit + d)
	case paneDiff:
		h, ok := m.curHunk()
		if !ok {
			return
		}
		_, lines := parseHunk(h)
		visible := m.mainHeight() - 3
		if d > 0 && m.diffScroll+visible < len(lines)+2 {
			m.diffScroll += 3
			return
		}
		if d < 0 && m.diffScroll > 0 {
			m.diffScroll = max(0, m.diffScroll-3)
			return
		}
		u, _ := m.curUnit()
		if m.hunk+d >= len(u.Hunks) || m.hunk+d < 0 {
			m.selectUnit(m.unit + d)
			if d < 0 {
				u2, _ := m.curUnit()
				m.selectHunk(len(u2.Hunks) - 1)
			}
			return
		}
		m.selectHunk(m.hunk + d)
	case paneWhy:
		m.whyScroll = max(0, m.whyScroll+d)
	}
}

func (m *Model) updateTimeline(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.data.Timeline)
	switch msg.String() {
	case "q", "esc", "t":
		m.view = viewMain
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.tlSel = min(m.tlSel+1, n-1)
	case "k", "up":
		m.tlSel = max(m.tlSel-1, 0)
	case "g", "home":
		m.tlSel = 0
	case "G", "end":
		m.tlSel = n - 1
	case "enter":
		if m.tlSel >= 0 && m.tlSel < n {
			m.stepID, m.stepScroll = m.data.Timeline[m.tlSel].Step.ID, 0
			m.prev, m.view = viewTimeline, viewStep
		}
	case "?":
		m.prev, m.view = viewTimeline, viewHelp
	}
	return m, nil
}

func (m *Model) updateStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.translate(msg.String()) {
	case "q", "esc":
		m.view = m.prev
		if m.view == viewStep {
			m.view = viewTimeline
		}
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.stepScroll++
	case "k", "up":
		m.stepScroll = max(0, m.stepScroll-1)
	case "g":
		m.stepScroll = 0
	case "G":
		m.stepScroll = 1 << 20
	case "enter":
		if hs := m.hunksOfStep(m.stepID); len(hs) > 0 && m.jumpToHunk(hs[0]) {
			m.view, m.focus = viewMain, paneDiff
		}
	}
	return m, nil
}

func (m *Model) updateFlags(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.data.Flags)
	switch msg.String() {
	case "q", "esc", "f":
		m.view = viewMain
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.flagSel = min(m.flagSel+1, n-1)
	case "k", "up":
		m.flagSel = max(m.flagSel-1, 0)
	case "g":
		m.flagSel = 0
	case "G":
		m.flagSel = n - 1
	case "enter":
		if m.flagSel >= 0 && m.flagSel < n {
			f := m.data.Flags[m.flagSel]
			if m.jumpToFlag(f) {
				m.flagCursor = m.flagSel
				m.view, m.focus = viewMain, paneDiff
			} else if f.TargetKind == "step" {
				m.stepID, m.stepScroll = f.TargetID, 0
				m.prev, m.view = viewFlags, viewStep
			} else {
				m.status = "this flag has no hunk"
			}
		}
	}
	return m, nil
}

func (m *Model) updateAsk(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.translate(msg.String()) {
	case "q", "esc":
		m.view = viewMain
	case "ctrl+c":
		return m, tea.Quit
	case "a":
		return m, m.openInput("ask", "ask> ")
	case "tab", "j", "down":
		if len(m.citations) > 0 {
			m.citeSel = (m.citeSel + 1) % len(m.citations)
		}
	case "shift+tab", "k", "up":
		if len(m.citations) > 0 {
			m.citeSel = (m.citeSel - 1 + len(m.citations)) % len(m.citations)
		}
	case "enter":
		if m.citeSel < len(m.citations) {
			c := m.citations[m.citeSel]
			if c.kind == "hunk" && m.jumpToHunk(c.id) {
				m.view, m.focus = viewMain, paneDiff
			} else if c.kind == "step" {
				m.stepID, m.stepScroll = c.id, 0
				m.prev, m.view = viewAsk, viewStep
			}
		}
	}
	return m, nil
}

func (m *Model) Selection() (unit, hunk int, focus int, view int) {
	return m.unit, m.hunk, int(m.focus), int(m.view)
}

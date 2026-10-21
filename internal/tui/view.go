package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

const (
	unitsWidth = 28
	whyWidth   = 42
)

func (m *Model) View() string {
	var body []string
	switch m.view {
	case viewTimeline:
		body = m.viewTimeline()
	case viewStep:
		body = m.viewStepDetail()
	case viewFlags:
		body = m.viewFlags()
	case viewAsk:
		body = m.viewAsk()
	case viewHelp:
		body = m.viewHelp()
	default:
		body = m.viewMain()
	}
	return strings.Join(append(append([]string{m.header()}, body...), m.footer()), "\n")
}

func (m *Model) header() string {
	d := m.data
	left := fmt.Sprintf(" xis · %s · %s (%s) · %d steps · %d files ", oneLine(d.Title, 40), d.Agent, d.Fidelity, d.Steps, d.Files)
	if strings.Contains(d.Status, "no_transcript") {
		left += "· no transcript "
	}
	right := fmt.Sprintf(" %d high · %d med ", d.flagCount(model.SevHigh), d.flagCount(model.SevMedium))
	fill := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if fill < 1 {
		return m.st.header.Render(fit(left+right, m.width))
	}
	return m.st.header.Render(left + strings.Repeat("─", fill) + right)
}

func (m *Model) footer() string {
	if m.inputMode != "" {
		return fit(m.input.View(), m.width)
	}
	if m.status != "" {
		return m.st.footer.Render(fit(" "+m.status, m.width))
	}
	keys := map[viewKind]string{
		viewMain:     " j/k move  tab pane  p/P prompt  i full prompt  enter expand  u understood  x question  t timeline  f flags  a ask  o editor  ? help  q quit",
		viewTimeline: " j/k move  enter open step  g/G top/bottom  esc back  q back",
		viewStep:     " j/k scroll  enter jump to hunk  esc back",
		viewFlags:    " j/k move  enter jump to hunk  esc back",
		viewAsk:      " tab/j/k select citation  enter jump  a ask again  esc back",
		viewHelp:     " esc back",
	}
	return m.st.footer.Render(fit(keys[m.view], m.width))
}

func (m *Model) paneTitle(title string, p pane) string {
	if m.focus == p {
		return m.st.focusTitle.Render(title)
	}
	return m.st.title.Render(title)
}

func join(cols ...[]string) []string {
	if len(cols) == 0 {
		return nil
	}
	out := make([]string, len(cols[0]))
	sep := "│"
	for i := range out {
		parts := make([]string, len(cols))
		for c := range cols {
			parts[c] = cols[c][i]
		}
		out[i] = strings.Join(parts, sep)
	}
	return out
}

func (m *Model) viewMain() []string {
	return append(m.panes(m.mainHeight()), m.promptPanel()...)
}

func (m *Model) panes(h int) []string {
	switch m.layout() {
	case 3:
		diffW := m.width - unitsWidth - whyWidth - 2
		return join(m.unitsPane(unitsWidth, h), m.diffPane(diffW, h), m.whyPane(whyWidth, h))
	case 2:
		rightW := m.width - unitsWidth - 1
		right := m.diffPane(rightW, h)
		if m.focus == paneWhy {
			right = m.whyPane(rightW, h)
		}
		return join(m.unitsPane(unitsWidth, h), right)
	}
	switch m.focus {
	case paneDiff:
		return m.diffPane(m.width, h)
	case paneWhy:
		return m.whyPane(m.width, h)
	}
	return m.unitsPane(m.width, h)
}

// promptPanel shows the selected prompt in full under the panes, in at most
// half of the height. "i" opens a longer prompt on its own screen.
func (m *Model) promptPanel() []string {
	t, ok := m.curTurn()
	if !ok {
		return nil
	}
	title := fmt.Sprintf("── PROMPT #%d (%d of %d) ", t.prompt.Seq, m.prompt+1, len(m.turns))
	lines := []string{m.st.focusTitle.Render(title) + m.st.dim.Render(strings.Repeat("─", max(m.width-lipgloss.Width(title), 0)))}
	text := wrap(t.prompt.Text, m.width-2)
	if limit := max(m.bodyHeight()/2, 4) - 1; len(text) > limit {
		more := len(text) - limit + 1
		text = append(text[:limit-1], m.st.dim.Render(fmt.Sprintf("… %d more lines · press i for the full prompt", more)))
	}
	for _, l := range text {
		lines = append(lines, fit(" "+l, m.width))
	}
	return lines
}

func statusGlyph(s string) string {
	switch s {
	case "understood":
		return "✔"
	case "questioned":
		return "?"
	}
	return " "
}

func intentGlyph(s string) string {
	switch s {
	case "addressed":
		return "✔"
	case "partial":
		return "◐"
	case "unaddressed":
		return "✗"
	}
	return "·"
}

func (m *Model) unitsPane(w, h int) []string {
	title := "UNITS"
	if t, ok := m.curTurn(); ok {
		title = fmt.Sprintf("UNITS · prompt #%d", t.prompt.Seq)
	}
	lines := []string{m.paneTitle(title, paneUnits)}
	if len(m.units) == 0 {
		lines = append(lines, m.st.dim.Render("no changes"))
	}
	// Only the units around the selection are rendered.
	listH := max(h/2, 3)
	start := max(0, m.unit-listH+2)
	for i := start; i < len(m.units) && i < start+listH; i++ {
		u := m.units[i]
		marker := " "
		if i == m.unit {
			marker = "▸"
		}
		flags := strings.Repeat("⚑", min(m.data.unitFlags(u), 3))
		row := fmt.Sprintf("%s%s%d %s", marker, statusGlyph(u.Status), i+1, u.Title)
		row = fit(row, w-len([]rune(flags))-1) + " " + m.st.high.Render(flags)
		if i == m.unit {
			row = m.st.sel.Render(fit(row, w))
		}
		lines = append(lines, row)
	}
	lines = append(lines, "")
	if len(m.data.Intent) > 0 {
		lines = append(lines, m.st.title.Render("INTENT"))
		for i, it := range m.data.Intent {
			lines = append(lines, fmt.Sprintf("%s #%d %s", intentGlyph(it.Status), i+1, oneLine(it.Text, w)))
		}
		lines = append(lines, "")
	}
	lines = append(lines, m.st.title.Render("PROMPTS (p/P)"))
	if len(m.turns) == 0 {
		lines = append(lines, m.st.dim.Render("none recorded"))
	} else {
		lines = append(lines, m.promptRow(-1, "All prompts", len(m.data.hunks()), w))
		for i, t := range m.turns {
			lines = append(lines, m.promptRow(i, fmt.Sprintf("#%d %s", t.prompt.Seq, t.prompt.Text), t.hunks, w))
		}
	}
	return block(lines, w, h, 0)
}

func (m *Model) promptRow(i int, text string, hunks int, w int) string {
	marker := "  "
	if i == m.prompt {
		marker = "▸ "
	}
	count := fmt.Sprintf(" %d", hunks)
	label := fit(marker+oneLine(text, w), w-len(count))
	if i == m.prompt {
		return m.st.sel.Render(label + count)
	}
	return label + m.st.dim.Render(count)
}

func (m *Model) diffPane(w, h int) []string {
	hk, ok := m.curHunk()
	if !ok {
		return block([]string{m.paneTitle("DIFF", paneDiff), m.st.dim.Render("Select a unit with hunks.")}, w, h, 0)
	}
	u, _ := m.curUnit()
	title := fmt.Sprintf("%s  [%s]  hunk %d/%d", hk.FilePath, hk.ChangeType, m.hunk+1, len(u.Hunks))
	lines := []string{m.paneTitle(fit(title, w), paneDiff)}
	header, _ := parseHunk(hk)
	body := m.dimOtherPrompts(hk, m.hl.render(hk, m.st))
	content := []string{m.st.dim.Render(header)}
	content = append(content, body...)
	for _, f := range m.data.flagsForHunk(hk) {
		sup := ""
		if f.Suppressed {
			sup = " (suppressed)"
		}
		content = append(content, "", m.st.severity(string(f.Severity)).Render(fmt.Sprintf("⚑ %s %s%s", f.Severity, f.RuleID, sup)))
		content = append(content, wrap("  "+f.Message, w)...)
	}
	return append(lines, block(content, w, h-1, m.diffScroll)...)
}

func (m *Model) whyPane(w, h int) []string {
	lines := []string{m.paneTitle("WHY", paneWhy)}
	hk, ok := m.curHunk()
	if !ok {
		return block(lines, w, h, 0)
	}
	add := func(s string) { lines = append(lines, wrap(s, w)...) }
	prov := m.data.Provenance[hk.ID]
	var stepIDs []string
	unattributed := 0
	for _, p := range prov {
		if p.StepID == model.Unattributed {
			unattributed += p.Lines
			continue
		}
		stepIDs = append(stepIDs, p.StepID)
	}
	if len(stepIDs) == 0 {
		add(m.st.warn.Render("No captured step wrote this hunk (unattributed)."))
	}
	var sd *store.SessionData
	var first model.Step
	for i, id := range stepIDs {
		st, d, ok := m.data.step(id)
		if !ok {
			continue
		}
		if i == 0 {
			first, sd = st, d
		}
		who := ""
		switch m.data.ActorKind[id] {
		case "human":
			who = " (human)"
		case "subagent":
			who = " (subagent)"
		}
		lines_ := 0
		for _, p := range prov {
			if p.StepID == id {
				lines_ = p.Lines
			}
		}
		add(fmt.Sprintf("Step %d · %s%s · %d lines", st.Seq, oneLine(st.Text, 60), who, lines_))
	}
	if unattributed > 0 && len(stepIDs) > 0 {
		add(m.st.dim.Render(fmt.Sprintf("%d lines unattributed", unattributed)))
	}
	if sd != nil {
		lines = append(lines, "")
		if r, ok := m.data.Rationales[hk.ID]; ok && r.AgentClaim != "" {
			badge := "unverified"
			if r.Verified {
				badge = "verified"
			}
			add(fmt.Sprintf("Agent says (%s):", badge))
			add(m.st.quote.Render(fmt.Sprintf("  %q", r.AgentClaim)))
		} else if near := sd.AgentTextNear(first.ID, hk.FilePath, 3); len(near) > 0 {
			add("Agent text near this edit:")
			for _, t := range near {
				add(m.st.quote.Render(fmt.Sprintf("  %q", oneLine(t.Text, 240))))
			}
		}
		if ins, ok := sd.InstructionFor(first.ID); ok {
			add(fmt.Sprintf("Instruction: #%d %q", ins.Seq, oneLine(ins.Text, 120)))
		}
		if ev := sd.EvidenceBefore(first.ID, 5); len(ev) > 0 {
			add("Evidence:")
			for _, e := range ev {
				s := fmt.Sprintf("  %s (s%d)", oneLine(e.Text, 50), e.Seq)
				if r, ok := sd.TestRunOf(e.ID); ok {
					s += " " + strings.ToUpper(r.Outcome)
				}
				add(s)
			}
		}
		for _, tr := range sd.TestRuns {
			if st, ok := sd.Step(tr.StepID); ok && st.Seq > first.Seq {
				add(fmt.Sprintf("  later: %s (s%d) %s", oneLine(tr.Command, 40), st.Seq, strings.ToUpper(tr.Outcome)))
			}
		}
	}
	lines = append(lines, "")
	if r, ok := m.data.Rationales[hk.ID]; ok && r.Explanation != "" {
		add("Explanation (" + r.Model + "): " + r.Explanation)
	} else if !m.data.LLMOn {
		add(m.st.dim.Render("Explanation: LLM analysis off"))
	} else {
		add(m.st.dim.Render("Explanation: run `xis analyze`"))
	}
	return block(lines, w, h, m.whyScroll)
}

func (m *Model) viewTimeline() []string {
	h := m.bodyHeight()
	lines := []string{m.st.title.Render("TIMELINE")}
	listH := h - 1
	start := max(0, min(m.tlSel-listH/2, len(m.data.Timeline)-listH))
	for i := start; i < len(m.data.Timeline) && i < start+listH; i++ {
		t := m.data.Timeline[i]
		label := string(t.Step.Kind)
		if t.Tool != "" {
			label = t.Tool
		}
		row := fmt.Sprintf("%4d %s%-12s %s", t.Step.Seq, strings.Repeat("  ", t.Depth), oneLine(label, 12), oneLine(t.Step.Text, 80))
		if t.Test != "" {
			row += " [test " + t.Test + "]"
		}
		if len(t.Files) > 0 {
			row += fmt.Sprintf("  → %d files · %d hunks survive", len(t.Files), t.Survive)
		}
		if t.Abandoned {
			row = m.st.dim.Render(row + " (abandoned)")
		}
		if i == m.tlSel {
			row = m.st.sel.Render(fit(row, m.width))
		}
		lines = append(lines, row)
	}
	return block(lines, m.width, h, 0)
}

func (m *Model) viewStepDetail() []string {
	h := m.bodyHeight()
	st, sd, ok := m.data.step(m.stepID)
	if !ok {
		return block([]string{"step not found"}, m.width, h, 0)
	}
	w := m.width
	var lines []string
	add := func(s string) { lines = append(lines, wrap(s, w)...) }
	add(m.st.title.Render(fmt.Sprintf("Step %d · %s · %s", st.Seq, st.Kind, st.TsStart.Local().Format("15:04:05"))))
	add("")
	add(st.Text)
	if tc, ok := sd.ToolCalls[st.ID]; ok {
		add("")
		add(m.st.title.Render(fmt.Sprintf("Tool %s (%s)", tc.ToolName, tc.Category)))
		add("Input: " + tc.Input)
		if tc.ExitCode != nil {
			add(fmt.Sprintf("Exit code: %d", *tc.ExitCode))
		}
		add("Output:")
		add(tc.Output)
	}
	for _, e := range sd.EditsOf(st.ID) {
		add("")
		add(m.st.title.Render(fmt.Sprintf("Edit %s (+%d -%d)", e.FilePath, e.Added, e.Removed)))
		add(e.Patch)
	}
	if hs := m.hunksOfStep(st.ID); len(hs) > 0 {
		add("")
		add(fmt.Sprintf("%d hunks of the change come from this step. Press enter to jump.", len(hs)))
	}
	return block(lines, w, h, m.stepScroll)
}

func (m *Model) viewFlags() []string {
	h := m.bodyHeight()
	lines := []string{m.st.title.Render(fmt.Sprintf("FLAGS (%d)", len(m.data.Flags)))}
	if len(m.data.Flags) == 0 {
		lines = append(lines, "no flags")
	}
	start := max(0, m.flagSel-(h-3))
	for i := start; i < len(m.data.Flags) && i < start+h-1; i++ {
		f := m.data.Flags[i]
		sup := ""
		if f.Suppressed {
			sup = " (suppressed: " + f.SuppressReason + ")"
		}
		row := m.st.severity(string(f.Severity)).Render(fmt.Sprintf("%-6s", f.Severity)) + fmt.Sprintf(" %-24s %s%s", f.RuleID, f.Message, sup)
		if i == m.flagSel {
			row = m.st.sel.Render(fit(row, m.width))
		}
		lines = append(lines, row)
	}
	return block(lines, m.width, h, 0)
}

func (m *Model) viewAsk() []string {
	h := m.bodyHeight()
	lines := append([]string{}, m.answer...)
	if m.asking {
		lines = append(lines, m.st.dim.Render("thinking…"))
	}
	if len(m.citations) > 0 {
		lines = append(lines, "", m.st.title.Render("Citations"))
		for i, c := range m.citations {
			row := fmt.Sprintf("  [%s:%s]", c.kind, c.id)
			if c.kind == "step" {
				if st, _, ok := m.data.step(c.id); ok {
					row += fmt.Sprintf(" step %d %s", st.Seq, oneLine(st.Text, 60))
				}
			}
			if i == m.citeSel {
				row = m.st.sel.Render(fit(row, m.width))
			}
			lines = append(lines, row)
		}
	}
	var out []string
	for _, l := range lines {
		out = append(out, wrap(l, m.width)...)
	}
	return block(out, m.width, h, max(0, len(out)-h))
}

func (m *Model) viewHelp() []string {
	rows := [][2]string{
		{"j / k", "move down / up in the focused pane"},
		{"tab", "focus the next pane"},
		{"enter", "expand: units → diff → why"},
		{"[ / ]", "previous / next review unit"},
		{"p / P", "show only the changes of the next / previous prompt, then all again"},
		{"i", "read the full text of the selected prompt"},
		{"g / G", "top / bottom"},
		{"n / N", "next / previous flag"},
		{"/", "search the hunks"},
		{"u", "mark the unit as understood"},
		{"x", "mark the unit as questioned"},
		{"t", "timeline of the session"},
		{"f", "list of flags"},
		{"a", "ask a question (needs an LLM)"},
		{"o", "open the file at the hunk in $EDITOR"},
		{"q", "quit (or go back)"},
	}
	lines := []string{m.st.title.Render("KEYS"), ""}
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("  %-8s %s", r[0], r[1]))
	}
	lines = append(lines, "", "Agent text is a claim, not a fact. \"verified\" means xis checked it against recorded evidence.")
	if !m.data.LLMOn {
		lines = append(lines, "LLM analysis is off. Units come from fixed rules.")
	}
	return block(lines, m.width, m.bodyHeight(), 0)
}

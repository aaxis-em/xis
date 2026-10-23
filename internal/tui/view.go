package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/textx"
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
	brand := " ∂ xis "
	left := fmt.Sprintf("· %s · %s (%s) · %d steps · %d files ", textx.OneLine(d.Title, 40), d.Agent, d.Fidelity, d.Steps, d.Files)
	if strings.Contains(d.Status, "no_transcript") {
		left += "· no transcript "
	}
	p := m.progressOf(m.all)
	right := fmt.Sprintf(" %d/%d understood · %d high · %d med ", p.understood, p.total, d.flagCount(model.SevHigh), d.flagCount(model.SevMedium))
	fill := m.width - lipgloss.Width(brand) - lipgloss.Width(left) - lipgloss.Width(right)
	if fill < 1 {
		return m.st.brand.Render(brand) + m.st.header.Render(fit(left+right, max(m.width-lipgloss.Width(brand), 0)))
	}
	return m.st.brand.Render(brand) + m.st.header.Render(left+strings.Repeat("─", fill)+right)
}

func (m *Model) footer() string {
	if m.inputMode != "" {
		return fit(m.input.View(), m.width)
	}
	if m.status != "" {
		return m.st.footer.Render(fit(" "+m.status, m.width))
	}
	keys := map[viewKind]string{
		viewMain:     " j/k move  tab pane  p/P prompt  i full prompt  enter expand  u/U understood (hunk/unit)  x question  t timeline  f flags  a ask  o editor  ? help  q quit",
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

// promptPanel is one line under the panes with the selected prompt. "i"
// opens the full prompt on its own screen.
func (m *Model) promptPanel() []string {
	t, ok := m.curTurn()
	if !ok {
		return nil
	}
	label := fmt.Sprintf(" PROMPT #%d (%d of %d) ", t.prompt.Seq, m.prompt+1, len(m.turns))
	hint := "  i: full prompt "
	text := fit(textx.OneLine(t.prompt.Text, m.width), max(m.width-lipgloss.Width(label)-len(hint), 0))
	return []string{m.st.focusTitle.Render(label) + text + m.st.dim.Render(hint)}
}

func (m *Model) progressGlyph(p progress) string {
	g := p.glyph()
	switch g {
	case "✔":
		return m.st.ok.Render(g)
	case "?":
		return m.st.warn.Render(g)
	}
	return g
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
	var intent []string
	if n := len(m.data.Intent); n > 0 {
		intent = append(intent, m.st.title.Render("INTENT"))
		for i, it := range m.data.Intent[:min(n, maxIntentRows)] {
			intent = append(intent, fmt.Sprintf("%s #%d %s", intentGlyph(it.Status), i+1, textx.OneLine(it.Text, w)))
		}
		if n > maxIntentRows {
			intent = append(intent, m.st.dim.Render(fmt.Sprintf("  … %d more", n-maxIntentRows)))
		}
		intent = append(intent, "")
	}
	prompts := len(m.turns) + 1
	if len(m.turns) == 0 {
		prompts = 1
	}
	unitsH, promptsH := splitHeight(h-3-len(intent), max(len(m.units), 1), prompts)

	lines := []string{m.paneTitle(title, paneUnits)}
	if len(m.units) == 0 {
		lines = append(lines, m.st.dim.Render("no changes"))
	} else {
		lines = append(lines, m.listRows(len(m.units), m.unit, unitsH, func(i int) string { return m.unitRow(i, w) })...)
	}
	lines = append(lines, "")
	lines = append(lines, intent...)
	lines = append(lines, m.st.title.Render("PROMPTS (p/P)"))
	if len(m.turns) == 0 {
		lines = append(lines, m.st.dim.Render("none recorded"))
	} else {
		lines = append(lines, m.listRows(prompts, m.prompt+1, promptsH, func(i int) string {
			if i == 0 {
				return m.promptRow(-1, "All prompts", len(m.all), w)
			}
			t := m.turns[i-1]
			return m.promptRow(i-1, fmt.Sprintf("#%d %s", t.prompt.Seq, t.prompt.Text), t.hunks, w)
		})...)
	}
	return block(lines, w, h, 0)
}

const maxIntentRows = 5

// splitHeight shares the free lines between the units and the prompts. Both
// lists get all their rows when they fit; else the prompts get up to two
// fifths, and a list that needs less gives the rest to the other.
func splitHeight(free, units, prompts int) (int, int) {
	free = max(free, 2)
	if units+prompts <= free {
		return units, prompts
	}
	p := min(prompts, max(3, free*2/5))
	u := max(free-p, 1)
	if units < u {
		p, u = min(prompts, p+u-units), units
	}
	return u, max(min(p, free-u), 1)
}

// listRows renders the rows of a list of n items in size lines and keeps
// item sel visible. A line at the top or the bottom tells how many items are
// hidden there.
func (m *Model) listRows(n, sel, size int, row func(int) string) []string {
	start, end := 0, n
	above, below := false, false
	switch {
	case n <= size:
	case size < 3:
		start = max(0, min(sel-size/2, n-size))
		end = start + size
	case sel <= (size-1)/2:
		end, below = size-1, true
	case sel >= n-1-size/2:
		start, above = n-size+1, true
	default:
		start, above, below = sel-(size-2)/2, true, true
		end = start + size - 2
	}
	var out []string
	if above {
		out = append(out, m.st.dim.Render(fmt.Sprintf("  ↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		out = append(out, row(i))
	}
	if below {
		out = append(out, m.st.dim.Render(fmt.Sprintf("  ↓ %d more", n-end)))
	}
	return out
}

func (m *Model) unitRow(i, w int) string {
	u := m.units[i]
	marker := " "
	if i == m.unit {
		marker = "▸"
	}
	flags := strings.Repeat("⚑", min(m.data.unitFlags(u), 3))
	p := m.progressOf(u.Hunks)
	count := ""
	if p.partial() {
		count = fmt.Sprintf("%d/%d", p.understood, p.total)
	}
	right := lipgloss.Width(flags) + len(count) + 1
	row := fmt.Sprintf("%s%s%d %s", marker, m.progressGlyph(p), i+1, u.Title)
	row = fit(row, w-right) + " " + m.st.dim.Render(count) + m.st.high.Render(flags)
	if i == m.unit {
		row = m.st.sel.Render(fit(row, w))
	}
	return row
}

func (m *Model) promptRow(i int, text string, hunks int, w int) string {
	marker := "  "
	if i == m.prompt {
		marker = "▸ "
	}
	count := fmt.Sprintf(" %d", hunks)
	label := fit(marker+textx.OneLine(text, w), w-len(count))
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
	switch m.hunkStatus(hk) {
	case "understood":
		title += "  ✔ understood"
	case "questioned":
		title += "  ? questioned"
	}
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
		add(fmt.Sprintf("Step %d · %s%s · %d lines", st.Seq, textx.OneLine(st.Text, 60), who, lines_))
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
				add(m.st.quote.Render(fmt.Sprintf("  %q", textx.OneLine(t.Text, 240))))
			}
		}
		if ins, ok := sd.InstructionFor(first.ID); ok {
			add(fmt.Sprintf("Instruction: #%d %q", ins.Seq, textx.OneLine(ins.Text, 120)))
		}
		if ev := sd.EvidenceBefore(first.ID, 5); len(ev) > 0 {
			add("Evidence:")
			for _, e := range ev {
				s := fmt.Sprintf("  %s (s%d)", textx.OneLine(e.Text, 50), e.Seq)
				if r, ok := sd.TestRunOf(e.ID); ok {
					s += " " + strings.ToUpper(r.Outcome)
				}
				add(s)
			}
		}
		for _, tr := range sd.TestRuns {
			if st, ok := sd.Step(tr.StepID); ok && st.Seq > first.Seq {
				add(fmt.Sprintf("  later: %s (s%d) %s", textx.OneLine(tr.Command, 40), st.Seq, strings.ToUpper(tr.Outcome)))
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
		row := fmt.Sprintf("%4d %s%-12s %s", t.Step.Seq, strings.Repeat("  ", t.Depth), textx.OneLine(label, 12), textx.OneLine(t.Step.Text, 80))
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
					row += fmt.Sprintf(" step %d %s", st.Seq, textx.OneLine(st.Text, 60))
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
		{"u", "mark the hunk as understood and go to the next hunk (again: clear)"},
		{"U", "mark all hunks of the unit as understood (again: clear)"},
		{"x", "mark the hunk as questioned (again: clear)"},
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

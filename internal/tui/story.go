package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/textx"
)

// A group is a heading of the story outline: one prompt, the hunks that no
// prompt explains, or the generated files. Its units are one file each.
type group struct {
	kind  string // prompt, none or generated
	turn  int
	units []int
}

// buildStory puts each hunk under every prompt that wrote it, file by file
// in the order the agent edited them. Hunks that no prompt wrote and
// generated files get groups of their own at the end, so nothing hides.
func (m *Model) buildStory() {
	m.units, m.groups, m.unitGroup = nil, nil, nil
	addGroup := func(g group, files []string, byFile map[string][]model.Hunk) {
		for _, f := range files {
			hs := byFile[f]
			sort.SliceStable(hs, func(i, j int) bool { return hs[i].NewStart < hs[j].NewStart })
			g.units = append(g.units, len(m.units))
			m.unitGroup = append(m.unitGroup, len(m.groups))
			m.units = append(m.units, Unit{ReviewUnit: model.ReviewUnit{ID: fmt.Sprintf("story:%d:%s", len(m.groups), f), Title: f}, Hunks: hs})
		}
		m.groups = append(m.groups, g)
	}
	written := map[string]bool{}
	for ti, t := range m.turns {
		byFile, first := map[string][]model.Hunk{}, map[string]int{}
		var files []string
		for _, h := range m.all {
			if h.ChangeType == model.ChangeGenerated || !t.wrote(m.data, h) {
				continue
			}
			written[h.ID] = true
			if _, ok := byFile[h.FilePath]; !ok {
				files, first[h.FilePath] = append(files, h.FilePath), 1<<30
			}
			byFile[h.FilePath] = append(byFile[h.FilePath], h)
			for _, p := range m.data.Provenance[h.ID] {
				if st, _, ok := m.data.step(p.StepID); ok && t.steps[p.StepID] {
					first[h.FilePath] = min(first[h.FilePath], st.Seq)
				}
			}
		}
		sort.SliceStable(files, func(i, j int) bool { return first[files[i]] < first[files[j]] })
		addGroup(group{kind: "prompt", turn: ti}, files, byFile)
	}
	rest := map[string]map[string][]model.Hunk{"none": {}, "generated": {}}
	for _, h := range m.all {
		kind := "none"
		if h.ChangeType == model.ChangeGenerated {
			kind = "generated"
		} else if written[h.ID] {
			continue
		}
		rest[kind][h.FilePath] = append(rest[kind][h.FilePath], h)
	}
	for _, kind := range []string{"none", "generated"} {
		if len(rest[kind]) == 0 {
			continue
		}
		files := make([]string, 0, len(rest[kind]))
		for f := range rest[kind] {
			files = append(files, f)
		}
		sort.Strings(files)
		addGroup(group{kind: kind, turn: -1}, files, rest[kind])
	}
}

func (m *Model) curGroup() int {
	if m.onGroup {
		return m.headGroup
	}
	if m.unit >= 0 && m.unit < len(m.unitGroup) {
		return m.unitGroup[m.unit]
	}
	return -1
}

func (m *Model) groupHunks(g group) []model.Hunk {
	var out []model.Hunk
	for _, u := range g.units {
		out = append(out, m.units[u].Hunks...)
	}
	return out
}

// outlineRow is a row of the outline: a group heading (unit -1) or a file.
type outlineRow struct{ group, unit int }

// outline lists every group heading and the files of the current group
// only, so the list stays short and follows the cursor.
func (m *Model) outline() ([]outlineRow, int) {
	cur, sel := m.curGroup(), 0
	var rows []outlineRow
	for gi, g := range m.groups {
		if gi == cur && m.onGroup {
			sel = len(rows)
		}
		rows = append(rows, outlineRow{gi, -1})
		if gi != cur {
			continue
		}
		for _, u := range g.units {
			if u == m.unit && !m.onGroup {
				sel = len(rows)
			}
			rows = append(rows, outlineRow{gi, u})
		}
	}
	return rows, sel
}

func (m *Model) moveOutline(d int) {
	rows, sel := m.outline()
	if len(rows) == 0 {
		return
	}
	m.setRow(rows[max(0, min(sel+d, len(rows)-1))])
}

func (m *Model) setRow(r outlineRow) {
	if r.unit < 0 {
		m.onGroup, m.headGroup = true, r.group
		m.diffScroll, m.whyScroll = 0, 0
		return
	}
	m.onGroup = false
	m.selectUnit(r.unit)
}

// nextGroup moves to the heading of the next prompt in direction d.
func (m *Model) nextGroup(d int) {
	n := len(m.groups)
	for i, g := 1, m.curGroup(); i <= n; i++ {
		next := ((g+d*i)%n + n) % n
		if m.groups[next].kind == "prompt" {
			m.setRow(outlineRow{next, -1})
			return
		}
	}
}

// nextUnreviewed selects the next hunk in story order that is not marked.
func (m *Model) nextUnreviewed() {
	type pos struct{ u, h int }
	var all []pos
	start := 0
	for ui, u := range m.units {
		for hi := range u.Hunks {
			if ui == m.unit && hi == m.hunk && !m.onGroup {
				start = len(all) + 1
			} else if m.onGroup && ui == firstUnit(m, m.headGroup) && hi == 0 {
				start = len(all)
			}
			all = append(all, pos{ui, hi})
		}
	}
	for i := range all {
		p := all[(start+i)%len(all)]
		if m.hunkStatus(m.units[p.u].Hunks[p.h]) == "unreviewed" {
			m.onGroup = false
			m.unit, m.hunk, m.diffScroll, m.whyScroll = p.u, p.h, 0, 0
			m.focus = paneDiff
			return
		}
	}
	m.status = "every hunk is marked"
}

func firstUnit(m *Model, g int) int {
	for i := g; i >= 0 && i < len(m.groups); i++ {
		if len(m.groups[i].units) > 0 {
			return m.groups[i].units[0]
		}
	}
	return -1
}

func (m *Model) groupTitle(g group) string {
	switch g.kind {
	case "none":
		return "? no prompt"
	case "generated":
		return fmt.Sprintf("⋯ %d generated files", len(g.units))
	}
	t := m.turns[g.turn]
	return fmt.Sprintf("#%d %s", t.prompt.Seq, textx.OneLine(t.prompt.Text, 200))
}

func (m *Model) storyPane(w, h int) []string {
	lines := []string{m.paneTitle("STORY", paneUnits)}
	if len(m.groups) == 0 {
		return block(append(lines, m.st.dim.Render("no changes")), w, h, 0)
	}
	rows, sel := m.outline()
	lines = append(lines, m.listRows(len(rows), sel, h-1, func(i int) string {
		r := rows[i]
		cursor := " "
		if i == sel {
			cursor = "›"
		}
		var row string
		if r.unit < 0 {
			row = m.groupRow(r.group, w-1)
		} else {
			row = m.fileRow(r.unit, w-1)
		}
		if i == sel {
			return m.st.sel.Render(fit(cursor+row, w))
		}
		return cursor + row
	})...)
	return block(lines, w, h, 0)
}

func (m *Model) groupRow(gi, w int) string {
	g := m.groups[gi]
	fold := "▸"
	if gi == m.curGroup() {
		fold = "▾"
	}
	hs := m.groupHunks(g)
	flags := 0
	for _, u := range g.units {
		flags += m.data.unitFlags(m.units[u])
	}
	right := fmt.Sprintf(" %s %d", m.progressGlyph(m.progressOf(hs)), len(hs))
	if flags > 0 {
		right = m.st.high.Render(" ⚑") + right
	}
	title := m.groupTitle(g)
	if g.kind == "none" {
		title = m.st.warn.Render(title)
	}
	return fit(fold+" "+title, w-lipgloss.Width(right)) + m.st.dim.Render(right)
}

func (m *Model) fileRow(ui, w int) string {
	u := m.units[ui]
	add, del := 0, 0
	for _, h := range u.Hunks {
		_, lines := parseHunk(h)
		for _, l := range lines {
			switch l.kind {
			case '+':
				add++
			case '-':
				del++
			}
		}
	}
	right := fmt.Sprintf(" +%d −%d", add, del)
	if m.data.unitFlags(u) > 0 {
		right = m.st.high.Render(" ⚑") + right
	}
	name := u.Title
	if room := w - 5 - lipgloss.Width(right); room > 1 && len(name) > room {
		name = "…" + name[len(name)-room+1:]
	}
	return fit("  "+m.progressGlyph(m.progressOf(u.Hunks))+" "+name, w-lipgloss.Width(right)) + m.st.dim.Render(right)
}

// storyCard tells what one prompt asked, what the agent said it did, and
// what it ran, before the reader looks at any diff.
func (m *Model) storyCard(w, h int) []string {
	g := m.groups[m.headGroup]
	var lines []string
	add := func(s string) { lines = append(lines, wrap(s, w)...) }
	hs := m.groupHunks(g)
	p := m.progressOf(hs)
	summary := fmt.Sprintf("%d hunks in %d files · %d understood", len(hs), len(g.units), p.understood)
	switch g.kind {
	case "none":
		add(m.st.warn.Render("NO PROMPT"))
		add(fmt.Sprintf("No recorded step wrote these %d hunks, so no prompt or agent text explains them. Read them with care.", len(hs)))
		m.importedNote(add)
		add("")
		add(m.st.dim.Render(summary + " · enter: read the changes"))
		return block(lines, w, h, m.whyScroll)
	case "generated":
		add(m.st.title.Render("GENERATED FILES"))
		add(fmt.Sprintf("%d hunks in lock files or generated files. xis puts them last, as they are rarely worth a line-by-line read.", len(hs)))
		add("")
		add(m.st.dim.Render(summary + " · enter: read the changes"))
		return block(lines, w, h, m.whyScroll)
	}
	t := m.turns[g.turn]
	_, sd, _ := m.data.step(t.prompt.ID)
	add(m.st.focusTitle.Render(fmt.Sprintf("PROMPT #%d", t.prompt.Seq)) + m.st.dim.Render(" · "+t.prompt.TsStart.Local().Format("15:04")))
	add(t.prompt.Text)
	steps := m.turnSteps(t, sd)
	var reply string
	for _, st := range steps {
		if st.Kind == model.StepAssistantMessage && strings.TrimSpace(st.Text) != "" {
			reply = st.Text
		}
	}
	if reply != "" {
		add("")
		add(m.st.title.Render("AGENT SAID") + m.st.dim.Render(" (a claim, not checked)"))
		quoted := wrap(reply, w-2)
		if len(quoted) > 8 {
			quoted = append(quoted[:8], "…")
		}
		for _, q := range quoted {
			lines = append(lines, m.st.quote.Render("  "+q))
		}
	}
	var did []string
	for _, st := range steps {
		if sd == nil {
			break
		}
		if edits := sd.EditsOf(st.ID); len(edits) > 0 {
			for _, e := range edits {
				did = append(did, fmt.Sprintf("  %3d  %-12s %s %s", st.Seq, textx.OneLine(toolName(sd, st), 12), e.FilePath, m.st.dim.Render(fmt.Sprintf("+%d −%d", e.Added, e.Removed))))
			}
		} else if r, ok := sd.TestRunOf(st.ID); ok {
			did = append(did, fmt.Sprintf("  %3d  %-12s %s %s", st.Seq, "test", textx.OneLine(r.Command, 40), m.outcome(r.Outcome)))
		}
	}
	add("")
	add(m.st.title.Render("WHAT IT DID"))
	if len(did) == 0 {
		add(m.st.dim.Render("  no edits and no test runs"))
	}
	for i, d := range did {
		if i == 12 {
			add(m.st.dim.Render(fmt.Sprintf("  … %d more (t: timeline)", len(did)-12)))
			break
		}
		add(d)
	}
	m.importedNote(add)
	add("")
	if len(hs) == 0 {
		add(m.st.dim.Render("This prompt changed no lines that are still in the change."))
	} else {
		add(m.st.dim.Render(summary + " · enter: read the changes · i: full prompt"))
	}
	return block(lines, w, h, m.whyScroll)
}

func (m *Model) importedNote(add func(string)) {
	if m.data.Fidelity == "imported" || m.data.Fidelity == "mixed" {
		add("")
		add(m.st.dim.Render("Imported session: xis cannot link edits that the agent made with shell commands. Install the hooks for live capture."))
	}
}

func (m *Model) turnSteps(t turn, sd *store.SessionData) []model.Step {
	if sd == nil {
		return nil
	}
	var out []model.Step
	for _, st := range sd.Steps {
		if t.steps[st.ID] {
			out = append(out, st)
		}
	}
	return out
}

func toolName(sd *store.SessionData, st model.Step) string {
	if tc, ok := sd.ToolCalls[st.ID]; ok {
		return tc.ToolName
	}
	return string(st.Kind)
}

func (m *Model) outcome(o string) string {
	o = strings.ToUpper(o)
	switch o {
	case "PASS", "PASSED":
		return m.st.ok.Render(o)
	case "FAIL", "FAILED", "ERROR":
		return m.st.high.Render(o)
	}
	return m.st.dim.Render(o)
}

// contextStrip is the few lines above a diff: the step that wrote the hunk,
// what the agent said just before, and whether a test ran after it.
func (m *Model) contextStrip(hk model.Hunk, w int) []string {
	var steps []string
	for _, p := range m.data.Provenance[hk.ID] {
		if p.StepID != model.Unattributed {
			steps = append(steps, p.StepID)
		}
	}
	if len(steps) == 0 {
		return []string{fit(m.st.warn.Render("No recorded step wrote this hunk.")+m.st.dim.Render(" w: why"), w)}
	}
	st, sd, ok := m.data.step(steps[0])
	if !ok {
		return nil
	}
	who := ""
	if k := m.data.ActorKind[st.ID]; k == "human" || k == "subagent" {
		who = " (" + k + ")"
	}
	first := fmt.Sprintf("step %d · %s%s", st.Seq, textx.OneLine(st.Text, 60), who)
	if len(steps) > 1 {
		first += fmt.Sprintf(" · and %d more steps", len(steps)-1)
	}
	tests := m.st.dim.Render(" · no test ran after it")
	for _, tr := range sd.TestRuns {
		if s, ok := sd.Step(tr.StepID); ok && s.Seq > st.Seq {
			tests = m.st.dim.Render(" · test after: ") + m.outcome(tr.Outcome)
		}
	}
	out := []string{fit(m.st.title.Render(first)+tests+m.st.dim.Render(" · w: why"), w)}
	if r, ok := m.data.Rationales[hk.ID]; ok && r.AgentClaim != "" {
		out = append(out, fit(m.st.quote.Render(fmt.Sprintf("“%s”", textx.OneLine(r.AgentClaim, w))), w))
	} else if near := sd.AgentTextNear(st.ID, hk.FilePath, 3); len(near) > 0 {
		out = append(out, fit(m.st.quote.Render(fmt.Sprintf("“%s”", textx.OneLine(near[len(near)-1].Text, w))), w))
	}
	return out
}

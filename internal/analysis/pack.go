package analysis

import (
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/aaxis-em/xis/internal/adapters"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

type Input struct {
	Change     model.Change
	Hunks      []model.Hunk
	Provenance map[string][]model.HunkProvenance
	Sessions   []*store.SessionData
	Units      []model.ReviewUnit
}

// Pack renders the session as delimited blocks with short aliases (s1, h1,
// i1) and maps the aliases back to real IDs. Every rendered string passes
// through the redactor again before it can reach a provider.
type Pack struct {
	in        Input
	redact    func(string) string
	sendCode  bool
	stepAlias map[string]string
	aliasStep map[string]string
	hunkAlias map[string]string
	aliasHunk map[string]string
	intAlias  map[string]string
	aliasInt  map[string]string
	steps     map[string]model.Step
	sessionOf map[string]*store.SessionData
	hunks     map[string]model.Hunk
	intent    []model.IntentItem
	Summaries map[string]string
}

func NewPack(in Input, redact func(string) string, sendCode bool) *Pack {
	if redact == nil {
		redact = func(s string) string { return s }
	}
	p := &Pack{in: in, redact: redact, sendCode: sendCode,
		stepAlias: map[string]string{}, aliasStep: map[string]string{}, hunkAlias: map[string]string{}, aliasHunk: map[string]string{},
		intAlias: map[string]string{}, aliasInt: map[string]string{}, steps: map[string]model.Step{}, sessionOf: map[string]*store.SessionData{},
		hunks: map[string]model.Hunk{}, Summaries: map[string]string{}}
	n := 0
	for _, d := range in.Sessions {
		for _, s := range d.Steps {
			n++
			a := fmt.Sprintf("s%d", n)
			p.stepAlias[s.ID], p.aliasStep[a] = a, s.ID
			p.steps[s.ID], p.sessionOf[s.ID] = s, d
			if s.Summary != "" {
				p.Summaries[s.ID] = s.Summary
			}
		}
	}
	for i, h := range in.Hunks {
		a := fmt.Sprintf("h%d", i+1)
		p.hunkAlias[h.ID], p.aliasHunk[a] = a, h.ID
		p.hunks[h.ID] = h
	}
	return p
}

func (p *Pack) SetIntent(items []model.IntentItem) {
	p.intent = items
	p.intAlias, p.aliasInt = map[string]string{}, map[string]string{}
	for i, it := range items {
		a := fmt.Sprintf("i%d", i+1)
		p.intAlias[it.ID], p.aliasInt[a] = a, it.ID
	}
}

func (p *Pack) StepID(alias string) string   { return p.aliasStep[alias] }
func (p *Pack) HunkID(alias string) string   { return p.aliasHunk[alias] }
func (p *Pack) IntentID(alias string) string { return p.aliasInt[alias] }

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n[... clipped ...]"
}

func attr(s string) string { return html.EscapeString(strings.ReplaceAll(s, "\n", " ")) }

// neutralize stops a log line from closing the block it sits in.
func neutralize(s string) string {
	for _, tag := range []string{"session_step", "hunk", "intent_item", "question_context"} {
		s = strings.ReplaceAll(s, "</"+tag, "<\\/"+tag)
		s = strings.ReplaceAll(s, "<"+tag, "<\\"+tag)
	}
	return s
}

func (p *Pack) actorKind(s model.Step) string {
	d := p.sessionOf[s.ID]
	if d == nil {
		return ""
	}
	return d.Actors[s.ActorID].Kind
}

// StepBlock renders one step. With summary true and a summary present, the
// block holds the summary in place of the full text.
func (p *Pack) StepBlock(id string, summary bool) string {
	s, ok := p.steps[id]
	if !ok {
		return ""
	}
	d := p.sessionOf[id]
	header := fmt.Sprintf(`<session_step id="%s" seq="%d" kind="%s" actor="%s"`, p.stepAlias[id], s.Seq, s.Kind, p.actorKind(s))
	tc, isTool := d.ToolCalls[id]
	if isTool {
		header += fmt.Sprintf(` tool="%s"`, attr(tc.ToolName))
	}
	var b strings.Builder
	text := s.Text
	if sum, ok := p.Summaries[id]; ok && summary {
		text = "(summary) " + sum
	}
	b.WriteString(clip(text, 3000))
	if isTool && !summary {
		if p.sendCode {
			b.WriteString("\ninput: " + clip(tc.Input, 1500))
			if tc.Output != "" {
				b.WriteString("\noutput: " + clip(tc.Output, 1500))
			}
		} else if cmd := adapters.Command(tc.Input); cmd != "" {
			b.WriteString("\ncommand: " + clip(cmd, 500))
		}
		if tc.ExitCode != nil {
			fmt.Fprintf(&b, "\nexit code: %d", *tc.ExitCode)
		}
		if r, ok := d.TestRunOf(id); ok {
			fmt.Fprintf(&b, "\ntest run: %s (%s)", r.Outcome, r.Command)
		}
	}
	for _, e := range d.EditsOf(id) {
		fmt.Fprintf(&b, "\nedited %s (+%d -%d)", e.FilePath, e.Added, e.Removed)
	}
	return header + ">\n" + p.redact(neutralize(b.String())) + "\n</session_step>"
}

func (p *Pack) HunkBlock(id string, withPatch bool) string {
	h, ok := p.hunks[id]
	if !ok {
		return ""
	}
	var steps []string
	for _, pr := range p.in.Provenance[id] {
		if a := p.stepAlias[pr.StepID]; a != "" {
			steps = append(steps, a)
		}
	}
	header := fmt.Sprintf(`<hunk id="%s" file="%s" type="%s" new_start="%d" new_lines="%d" old_lines="%d" steps="%s">`,
		p.hunkAlias[id], attr(h.FilePath), h.ChangeType, h.NewStart, h.NewLines, h.OldLines, strings.Join(steps, ","))
	body := ""
	if withPatch && p.sendCode {
		body = "\n" + p.redact(neutralize(clip(h.Patch, 4000)))
	}
	return header + body + "\n</hunk>"
}

func (p *Pack) IntentBlock() string {
	var b strings.Builder
	for _, it := range p.intent {
		fmt.Fprintf(&b, "<intent_item id=\"%s\" source=\"%s\">%s</intent_item>\n", p.intAlias[it.ID], it.Source, neutralize(it.Text))
	}
	return p.redact(b.String())
}

// ContextSteps returns the provenance steps of a hunk and the 3 steps before
// each of them, in order.
func (p *Pack) ContextSteps(hunkID string, before int) []string {
	seen := map[string]bool{}
	var out []string
	for _, pr := range p.in.Provenance[hunkID] {
		d := p.sessionOf[pr.StepID]
		if d == nil {
			continue
		}
		i := d.StepIndex[pr.StepID]
		for j := max(0, i-before); j <= i; j++ {
			id := d.Steps[j].ID
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	order := map[*store.SessionData]int{}
	for i, d := range p.in.Sessions {
		order[d] = i
	}
	sort.SliceStable(out, func(a, b int) bool {
		sa, sb := order[p.sessionOf[out[a]]], order[p.sessionOf[out[b]]]
		if sa != sb {
			return sa < sb
		}
		return p.steps[out[a]].Seq < p.steps[out[b]].Seq
	})
	return out
}

func (p *Pack) Steps() []model.Step {
	var out []model.Step
	for _, d := range p.in.Sessions {
		out = append(out, d.Steps...)
	}
	return out
}

func (p *Pack) Step(id string) (model.Step, bool) {
	s, ok := p.steps[id]
	return s, ok
}

// ClaimSteps returns the agent text steps that may hold a claim about a hunk:
// assistant, reasoning and plan steps of the same agent actor among each
// provenance step and the n steps before it. Human edits have none.
func (p *Pack) ClaimSteps(hunkID string, n int) map[string]bool {
	out := map[string]bool{}
	for _, pr := range p.in.Provenance[hunkID] {
		d := p.sessionOf[pr.StepID]
		if d == nil {
			continue
		}
		prov := p.steps[pr.StepID]
		if kind := d.Actors[prov.ActorID].Kind; kind != "agent" && kind != "subagent" {
			continue
		}
		i := d.StepIndex[pr.StepID]
		for j := max(0, i-n); j <= i; j++ {
			s := d.Steps[j]
			if s.ActorID == prov.ActorID && (s.Kind == model.StepAssistantMessage || s.Kind == model.StepReasoning || s.Kind == model.StepPlan) {
				out[s.ID] = true
			}
		}
	}
	return out
}

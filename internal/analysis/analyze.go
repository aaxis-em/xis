package analysis

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/analysis/prompts"
	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/llm"
	"github.com/aaxis-em/xis/internal/model"
)

type Analyzer struct {
	LLM      *llm.Client
	Redact   func(string) string
	SendCode bool
	// LongSessionChars is the size above which steps are summarized first.
	LongSessionChars int
	Now              func() time.Time
}

type Output struct {
	Intent           []model.IntentItem
	Units            []model.ReviewUnit
	HunkUnit         map[string]string
	GroupingFallback bool
	Rationales       []model.Rationale
	HunkIntent       []model.HunkIntent
	StepSummaries    map[string]string
	Model            string
	Warnings         []string
}

func (a *Analyzer) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now().UTC()
}

func schema(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func str() map[string]any             { return map[string]any{"type": "string"} }
func arr(item any) map[string]any     { return map[string]any{"type": "array", "items": item} }
func enum(v ...string) map[string]any { return map[string]any{"type": "string", "enum": v} }

func (a *Analyzer) call(ctx context.Context, task, version, system, user string, sch map[string]any, out any, model *string) error {
	req := llm.Request{Task: task, PromptVersion: version, System: system, Messages: []llm.Message{{Role: "user", Content: a.Redact(user)}}, Schema: sch, MaxTokens: 8192}
	resp, err := a.LLM.CompleteJSON(ctx, req, out)
	if err == nil && resp.Model != "" {
		*model = resp.Model
	}
	return err
}

// Run executes all tasks. A failed task adds a warning; the deterministic
// results stay in place.
func (a *Analyzer) Run(ctx context.Context, in Input) (*Output, error) {
	if a.Redact == nil {
		a.Redact = func(s string) string { return s }
	}
	p := NewPack(in, a.Redact, a.SendCode)
	out := &Output{HunkUnit: map[string]string{}, StepSummaries: map[string]string{}}
	if err := a.summarize(ctx, p, out); err != nil {
		if isFatal(err) {
			return out, err
		}
		out.Warnings = append(out.Warnings, "summaries: "+err.Error())
	}
	if err := a.intent(ctx, p, in, out); err != nil {
		if isFatal(err) {
			return out, err
		}
		out.Warnings = append(out.Warnings, "intent: "+err.Error())
	}
	p.SetIntent(out.Intent)
	if err := a.group(ctx, p, in, out); err != nil {
		if isFatal(err) {
			return out, err
		}
		out.Warnings = append(out.Warnings, "grouping: "+err.Error())
	}
	if err := a.rationales(ctx, p, in, out); err != nil {
		if isFatal(err) {
			return out, err
		}
		out.Warnings = append(out.Warnings, "rationale: "+err.Error())
	}
	if err := a.status(ctx, p, out); err != nil {
		if isFatal(err) {
			return out, err
		}
		out.Warnings = append(out.Warnings, "intent status: "+err.Error())
	}
	return out, nil
}

func isFatal(err error) bool {
	return errors.Is(err, llm.ErrBudget) || errors.Is(err, context.Canceled)
}

func (a *Analyzer) summarize(ctx context.Context, p *Pack, out *Output) error {
	limit := a.LongSessionChars
	if limit <= 0 {
		limit = 60000
	}
	total := 0
	for _, s := range p.Steps() {
		total += len(p.StepBlock(s.ID, false))
	}
	if total <= limit {
		return nil
	}
	var chunk []string
	size := 0
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		var res struct {
			Steps []struct {
				Step    string `json:"step"`
				Summary string `json:"summary"`
			} `json:"steps"`
		}
		sch := schema(map[string]any{"steps": arr(schema(map[string]any{"step": str(), "summary": str()}, "step", "summary"))}, "steps")
		if err := a.call(ctx, "summary", prompts.SummaryVersion, prompts.Summary, strings.Join(chunk, "\n"), sch, &res, &out.Model); err != nil {
			return err
		}
		for _, s := range res.Steps {
			if id := p.StepID(s.Step); id != "" && strings.TrimSpace(s.Summary) != "" {
				sum := oneLine(s.Summary, 300)
				p.Summaries[id], out.StepSummaries[id] = sum, sum
			}
		}
		chunk, size = nil, 0
		return nil
	}
	for _, s := range p.Steps() {
		if _, done := p.Summaries[s.ID]; done {
			continue
		}
		b := p.StepBlock(s.ID, false)
		if size+len(b) > 40000 {
			if err := flush(); err != nil {
				return err
			}
		}
		chunk, size = append(chunk, b), size+len(b)
	}
	return flush()
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n]
	}
	return s
}

func (a *Analyzer) intent(ctx context.Context, p *Pack, in Input, out *Output) error {
	allowed := map[string]string{}
	var blocks []string
	for _, d := range in.Sessions {
		prompts := 0
		for _, s := range d.Steps {
			src := ""
			switch {
			case s.Kind == model.StepUserMessage && d.Actors[s.ActorID].Kind == "human":
				src = "prompt"
				if prompts > 0 {
					src = "followup"
				}
				prompts++
			case s.Kind == model.StepPlan:
				src = "plan"
			case s.Kind == model.StepToolCall && d.ToolCalls[s.ID].ToolName == "TodoWrite":
				src = "todo"
			}
			if src == "" {
				continue
			}
			allowed[s.ID] = src
			blk := p.StepBlock(s.ID, false)
			if src == "todo" {
				blk = strings.Replace(blk, "</session_step>", "todos: "+p.redact(neutralize(clip(d.ToolCalls[s.ID].Input, 3000)))+"\n</session_step>", 1)
			}
			blocks = append(blocks, blk)
		}
	}
	if len(blocks) == 0 {
		return nil
	}
	var res struct {
		Items []struct {
			SourceStep string `json:"source_step"`
			Text       string `json:"text"`
		} `json:"items"`
	}
	sch := schema(map[string]any{"items": arr(schema(map[string]any{"source_step": str(), "text": str()}, "source_step", "text"))}, "items")
	if err := a.call(ctx, "intent", prompts.IntentVersion, prompts.Intent, strings.Join(blocks, "\n"), sch, &res, &out.Model); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, it := range res.Items {
		id := p.StepID(it.SourceStep)
		src, ok := allowed[id]
		text := oneLine(it.Text, 300)
		if !ok || text == "" || seen[id+text] {
			continue
		}
		seen[id+text] = true
		step, _ := p.Step(id)
		out.Intent = append(out.Intent, model.IntentItem{
			ID: model.StableID(step.TsStart, step.SessionID, "intent", id, text), SessionID: step.SessionID,
			Source: src, SourceStepID: id, Text: text, Status: "unknown",
		})
	}
	return nil
}

func (a *Analyzer) group(ctx context.Context, p *Pack, in Input, out *Output) error {
	fallback := func() {
		out.GroupingFallback = true
		out.Units = nil
	}
	if len(in.Hunks) == 0 {
		return nil
	}
	var blocks []string
	for _, h := range in.Hunks {
		blocks = append(blocks, p.HunkBlock(h.ID, true))
	}
	user := strings.Join(blocks, "\n")
	if ib := p.IntentBlock(); ib != "" {
		user = ib + "\n" + user
	}
	var res struct {
		Units []struct {
			Title string   `json:"title"`
			Hunks []string `json:"hunks"`
		} `json:"units"`
	}
	sch := schema(map[string]any{"units": arr(schema(map[string]any{"title": str(), "hunks": arr(str())}, "title", "hunks"))}, "units")
	if err := a.call(ctx, "group", prompts.GroupVersion, prompts.Group, user, sch, &res, &out.Model); err != nil {
		fallback()
		return err
	}
	assigned := map[string]string{}
	var units []model.ReviewUnit
	for i, u := range res.Units {
		title := oneLine(u.Title, 80)
		if title == "" || len(u.Hunks) == 0 {
			fallback()
			return fmt.Errorf("unit %d has no title or no hunks; using fixed grouping", i+1)
		}
		var ids []string
		for _, ha := range u.Hunks {
			id := p.HunkID(ha)
			if id == "" {
				fallback()
				return fmt.Errorf("unknown hunk %q; using fixed grouping", ha)
			}
			if _, dup := assigned[id]; dup {
				fallback()
				return fmt.Errorf("hunk %s is in two units; using fixed grouping", ha)
			}
			ids = append(ids, id)
		}
		sorted := append([]string{}, ids...)
		sort.Strings(sorted)
		uid := attribution.UnitID(in.Change.ID, "llm:"+strings.Join(sorted, ","))
		for _, id := range ids {
			assigned[id] = uid
		}
		units = append(units, model.ReviewUnit{ID: uid, ChangeID: in.Change.ID, Title: title, Ord: i + 1, Source: "llm"})
	}
	if len(assigned) != len(in.Hunks) {
		fallback()
		return fmt.Errorf("%d of %d hunks have no unit; using fixed grouping", len(in.Hunks)-len(assigned), len(in.Hunks))
	}
	out.Units, out.HunkUnit = units, assigned
	return nil
}

type rationaleItem struct {
	Hunk          string   `json:"hunk"`
	AgentClaim    string   `json:"agent_claim"`
	ClaimStep     string   `json:"claim_step"`
	Explanation   string   `json:"explanation"`
	EvidenceSteps []string `json:"evidence_steps"`
	IntentItems   []string `json:"intent_items"`
}

func (a *Analyzer) rationales(ctx context.Context, p *Pack, in Input, out *Output) error {
	units := map[string][]model.Hunk{}
	var order []string
	for _, h := range in.Hunks {
		key := out.HunkUnit[h.ID]
		if key == "" {
			key = h.ReviewUnitID
		}
		if _, ok := units[key]; !ok {
			order = append(order, key)
		}
		units[key] = append(units[key], h)
	}
	sch := schema(map[string]any{"hunks": arr(schema(map[string]any{
		"hunk": str(), "agent_claim": str(), "claim_step": str(), "explanation": str(),
		"evidence_steps": arr(str()), "intent_items": arr(str()),
	}, "hunk", "agent_claim", "claim_step", "explanation", "evidence_steps", "intent_items"))}, "hunks")
	var firstErr error
	for _, key := range order {
		hunks := units[key]
		for start := 0; start < len(hunks); start += 6 {
			batch := hunks[start:min(start+6, len(hunks))]
			if err := a.rationaleBatch(ctx, p, batch, sch, out); err != nil {
				if isFatal(err) {
					return err
				}
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

func (a *Analyzer) rationaleBatch(ctx context.Context, p *Pack, batch []model.Hunk, sch map[string]any, out *Output) error {
	var blocks []string
	seen := map[string]bool{}
	claimSteps := map[string]map[string]bool{}
	for _, h := range batch {
		claimSteps[h.ID] = p.ClaimSteps(h.ID, 3)
		for _, id := range p.ContextSteps(h.ID, 3) {
			if !seen[id] {
				seen[id] = true
				blocks = append(blocks, p.StepBlock(id, false))
			}
		}
	}
	if ib := p.IntentBlock(); ib != "" {
		blocks = append([]string{ib}, blocks...)
	}
	for _, h := range batch {
		blocks = append(blocks, p.HunkBlock(h.ID, true))
	}
	var res struct {
		Hunks []rationaleItem `json:"hunks"`
	}
	if err := a.call(ctx, "rationale", prompts.RationaleVersion, prompts.Rationale, strings.Join(blocks, "\n"), sch, &res, &out.Model); err != nil {
		return err
	}
	inBatch := map[string]bool{}
	for _, h := range batch {
		inBatch[h.ID] = true
	}
	for _, it := range res.Hunks {
		hid := p.HunkID(it.Hunk)
		if !inBatch[hid] {
			continue
		}
		r := model.Rationale{HunkID: hid, Model: out.Model, CreatedAt: a.now()}
		claimID := p.StepID(it.ClaimStep)
		claim := strings.TrimSpace(it.AgentClaim)
		if claim != "" && claimSteps[hid][claimID] {
			if step, ok := p.Step(claimID); ok && QuoteIn(claim, step.Text) && len(claim) <= 300 {
				r.AgentClaim, r.ClaimStepID = claim, claimID
				r.Verified = Verify(claim, step, p.sessionOf[claimID])
			}
		}
		r.Explanation, _ = Validate(it.Explanation, p.StepID, p.HunkID, false)
		for _, e := range it.EvidenceSteps {
			if id := p.StepID(e); id != "" {
				r.EvidenceStepIDs = append(r.EvidenceStepIDs, id)
			}
		}
		out.Rationales = append(out.Rationales, r)
		for _, ia := range it.IntentItems {
			if iid := p.IntentID(ia); iid != "" {
				out.HunkIntent = append(out.HunkIntent, model.HunkIntent{HunkID: hid, IntentItemID: iid, Confidence: 0.5})
			}
		}
	}
	return nil
}

// QuoteIn reports whether quote appears in text, ignoring differences in whitespace.
func QuoteIn(quote, text string) bool {
	q := strings.Join(strings.Fields(quote), " ")
	return q != "" && strings.Contains(strings.Join(strings.Fields(text), " "), q)
}

func (a *Analyzer) status(ctx context.Context, p *Pack, out *Output) error {
	if len(out.Intent) == 0 {
		return nil
	}
	var blocks []string
	blocks = append(blocks, p.IntentBlock())
	expl := map[string]string{}
	for _, r := range out.Rationales {
		expl[r.HunkID] = r.Explanation
	}
	for _, h := range p.in.Hunks {
		b := p.HunkBlock(h.ID, false)
		if e := expl[h.ID]; e != "" && e != NoEvidence {
			b = strings.Replace(b, "</hunk>", "explanation: "+p.redact(neutralize(e))+"\n</hunk>", 1)
		}
		blocks = append(blocks, b)
	}
	var res struct {
		Items []struct {
			Intent string   `json:"intent"`
			Status string   `json:"status"`
			Hunks  []string `json:"hunks"`
		} `json:"items"`
	}
	sch := schema(map[string]any{"items": arr(schema(map[string]any{"intent": str(), "status": enum("addressed", "partial", "unaddressed"), "hunks": arr(str())}, "intent", "status", "hunks"))}, "items")
	if err := a.call(ctx, "status", prompts.StatusVersion, prompts.Status, strings.Join(blocks, "\n"), sch, &res, &out.Model); err != nil {
		return err
	}
	idx := map[string]int{}
	for i, it := range out.Intent {
		idx[it.ID] = i
	}
	for _, it := range res.Items {
		id := p.IntentID(it.Intent)
		i, ok := idx[id]
		if !ok {
			continue
		}
		var hunks []string
		for _, ha := range it.Hunks {
			if hid := p.HunkID(ha); hid != "" {
				hunks = append(hunks, hid)
				out.HunkIntent = append(out.HunkIntent, model.HunkIntent{HunkID: hid, IntentItemID: id, Confidence: 0.8})
			}
		}
		switch it.Status {
		case "addressed", "partial":
			if len(hunks) > 0 {
				out.Intent[i].Status = it.Status
			}
		case "unaddressed":
			out.Intent[i].Status = "unaddressed"
		}
	}
	return nil
}

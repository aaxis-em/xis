package qa

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/analysis"
	"github.com/aaxis-em/xis/internal/analysis/prompts"
	"github.com/aaxis-em/xis/internal/llm"
	"github.com/aaxis-em/xis/internal/model"
)

type Asker struct {
	LLM      *llm.Client
	Redact   func(string) string
	SendCode bool
	MaxChars int
}

var wordRe = regexp.MustCompile(`[A-Za-z0-9_]{3,}`)

var stop = map[string]bool{"the": true, "and": true, "for": true, "why": true, "what": true, "did": true, "does": true, "this": true,
	"that": true, "with": true, "was": true, "were": true, "how": true, "are": true, "agent": true, "change": true, "from": true}

func terms(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		if !stop[w] {
			out[w] = true
		}
	}
	return out
}

func score(q map[string]bool, text string) int {
	n := 0
	for w := range terms(text) {
		if q[w] {
			n++
		}
	}
	return n
}

// Ask selects the records that share words with the question, so the
// provider never gets the whole transcript.
func (a *Asker) Ask(ctx context.Context, in analysis.Input, intent []model.IntentItem, question string) (model.QA, error) {
	if a.Redact == nil {
		a.Redact = func(s string) string { return s }
	}
	limit := a.MaxChars
	if limit <= 0 {
		limit = 60000
	}
	p := analysis.NewPack(in, a.Redact, a.SendCode)
	p.SetIntent(intent)
	q := terms(question)

	type cand struct {
		block string
		score int
		order int
		must  bool
	}
	var cands []cand
	steps := p.Steps()
	lastAgent := ""
	for _, s := range steps {
		if s.Kind == model.StepAssistantMessage {
			lastAgent = s.ID
		}
	}
	for i, s := range steps {
		sc := score(q, s.Text)
		must := s.Kind == model.StepUserMessage || s.ID == lastAgent
		cands = append(cands, cand{block: p.StepBlock(s.ID, !must && sc == 0), score: sc, order: i, must: must})
	}
	for i, h := range in.Hunks {
		sc := score(q, h.FilePath+" "+h.Patch)
		cands = append(cands, cand{block: p.HunkBlock(h.ID, sc > 0), score: sc, order: len(steps) + i})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].must != cands[j].must {
			return cands[i].must
		}
		return cands[i].score > cands[j].score
	})
	var chosen []cand
	size := 0
	for _, c := range cands {
		if size+len(c.block) > limit && !c.must {
			continue
		}
		chosen = append(chosen, c)
		size += len(c.block)
	}
	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].order < chosen[j].order })
	var b strings.Builder
	b.WriteString(p.IntentBlock())
	for _, c := range chosen {
		b.WriteString(c.block + "\n")
	}
	b.WriteString("<question_context>\nQuestion of the reviewer: " + a.Redact(question) + "\n</question_context>")

	req := llm.Request{Task: "ask", PromptVersion: prompts.AskVersion, System: prompts.Ask, MaxTokens: 4096,
		Messages: []llm.Message{{Role: "user", Content: a.Redact(b.String())}},
		Schema:   map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}, "required": []string{"answer"}, "additionalProperties": false}}
	var res struct {
		Answer string `json:"answer"`
	}
	resp, err := a.LLM.CompleteJSON(ctx, req, &res)
	if err != nil {
		return model.QA{}, err
	}
	answer, cites := analysis.Validate(res.Answer, p.StepID, p.HunkID, true)
	return model.QA{
		ID: model.NewID(), ChangeID: in.Change.ID, Question: question, Answer: answer,
		CitedStepIDs: cites.Steps, CitedHunkIDs: cites.Hunks, Model: resp.Model, CreatedAt: time.Now().UTC(),
	}, nil
}

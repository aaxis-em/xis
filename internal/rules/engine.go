package rules

import (
	"sort"
	"strings"

	"github.com/aaxis-em/xis/internal/model"
)

type Rule interface {
	ID() string
	Severity() model.Severity
	Check(ctx *Context) []model.Flag
}

var registry []Rule

func Register(r Rule) { registry = append(registry, r) }

func All() []Rule {
	out := append([]Rule{}, registry...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

type rule struct {
	id    string
	sev   model.Severity
	check func(r *rule, c *Context) []model.Flag
}

func (r *rule) ID() string                    { return r.id }
func (r *rule) Severity() model.Severity      { return r.sev }
func (r *rule) Check(c *Context) []model.Flag { return r.check(r, c) }
func def(id string, sev model.Severity, check func(r *rule, c *Context) []model.Flag) {
	Register(&rule{id: id, sev: sev, check: check})
}

func (r *rule) flag(c *Context, session, kind, target, key, msg string, evidence ...string) model.Flag {
	if key == "" {
		key = target
	}
	return model.Flag{
		ID:              model.StableID(zeroTime, c.Change.ID, r.id, kind, target),
		ChangeID:        c.Change.ID,
		SessionID:       session,
		RuleID:          r.id,
		Severity:        r.sev,
		Detector:        "rule",
		TargetKind:      kind,
		TargetID:        target,
		Message:         msg,
		EvidenceStepIDs: dedupe(evidence),
		Fingerprint:     strings.Join([]string{r.id, session, kind, key}, "|"),
	}
}

// hunkFlag points to line, or to the start of the hunk when line is 0.
func (r *rule) hunkFlag(c *Context, h model.Hunk, line int, msg string, evidence ...string) model.Flag {
	key := h.FilePath + "#" + h.ContentHash
	f := r.flag(c, c.SessionOfHunk(h), "hunk", h.ID, key, msg, append(evidence, c.AgentSteps(h)...)...)
	f.Line = line
	return f
}

func (r *rule) fileFlag(c *Context, path, msg string, evidence ...string) model.Flag {
	session := ""
	for _, h := range c.HunksOf(path) {
		if s := c.SessionOfHunk(h); s != "" {
			session = s
			evidence = append(evidence, c.AgentSteps(h)...)
		}
	}
	if session == "" && len(c.Change.SessionIDs) > 0 {
		session = c.Change.SessionIDs[0]
	}
	return r.flag(c, session, "file", path, path, msg, evidence...)
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		if id != "" && id != model.Unattributed && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Run applies all enabled rules with the severity overrides of the config.
func Run(c *Context) []model.Flag {
	disabled := map[string]bool{}
	for _, id := range c.Cfg.Rules.Disabled {
		disabled[strings.ToUpper(strings.TrimSpace(id))] = true
	}
	var out []model.Flag
	seen := map[string]bool{}
	for _, r := range All() {
		if disabled[r.ID()] {
			continue
		}
		for _, f := range r.Check(c) {
			if sev, ok := c.Cfg.Rules.Severity[r.ID()]; ok {
				f.Severity = model.Severity(sev)
			}
			if seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity.Rank() != out[j].Severity.Rank() {
			return out[i].Severity.Rank() > out[j].Severity.Rank()
		}
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		return out[i].TargetID < out[j].TargetID
	})
	return out
}

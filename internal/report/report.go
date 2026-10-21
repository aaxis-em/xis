package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

type Input struct {
	Change      model.Change
	Hunks       []model.Hunk
	Files       []gitx.FilePatch
	Provenance  map[string][]model.HunkProvenance
	ActorKind   map[string]string
	Sessions    []*store.SessionData
	Flags       []model.Flag
	Units       []model.ReviewUnit
	Rationales  map[string]model.Rationale
	Location    func(model.Flag) string
	XisVersion  string
	NoteRef     string
	GeneratedAt time.Time
}

type Session struct {
	ID       string   `json:"id"`
	Agent    string   `json:"agent"`
	Version  string   `json:"agent_version"`
	Models   []string `json:"models"`
	Fidelity string   `json:"fidelity"`
	Steps    int      `json:"steps"`
	Duration string   `json:"duration"`
	Status   string   `json:"status"`
}

type Intent struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

type Flag struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Location string `json:"location"`
	Message  string `json:"message"`
	Reason   string `json:"suppress_reason,omitempty"`
}

type Unit struct {
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Source      string   `json:"source"`
	Explanation []string `json:"explanation"`
	Files       []string `json:"files"`
	Hunks       int      `json:"hunks"`
}

type TestRun struct {
	Command string `json:"command"`
	Outcome string `json:"outcome"`
	Step    int    `json:"step"`
}

type Claim struct {
	Quote    string `json:"quote"`
	File     string `json:"file"`
	Verified bool   `json:"verified"`
}

type Report struct {
	ChangeID     string         `json:"change_id"`
	Base         string         `json:"base"`
	Target       string         `json:"target"`
	Sessions     []Session      `json:"sessions"`
	FilesChanged int            `json:"files_changed"`
	LinesAdded   int            `json:"lines_added"`
	LinesRemoved int            `json:"lines_removed"`
	Contributors map[string]int `json:"contributors"`
	Intent       []Intent       `json:"intent"`
	IntentNote   string         `json:"intent_note,omitempty"`
	Flags        []Flag         `json:"flags"`
	Suppressed   []Flag         `json:"suppressed"`
	Units        []Unit         `json:"review_units"`
	TestRuns     []TestRun      `json:"test_runs"`
	LastTestRun  string         `json:"last_test_run"`
	Claims       []Claim        `json:"agent_claims"`
	XisVersion   string         `json:"xis_version"`
	NoteRef      string         `json:"note_ref"`
	GeneratedAt  time.Time      `json:"generated_at"`
}

func duration(s model.Session) string {
	d := s.EndedAt.Sub(s.StartedAt).Round(time.Second)
	if d <= 0 {
		return "-"
	}
	return d.String()
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func Build(in Input) Report {
	r := Report{ChangeID: in.Change.ID, Base: short(in.Change.BaseRef), Target: short(in.Change.TargetRef), XisVersion: in.XisVersion,
		NoteRef: in.NoteRef, GeneratedAt: in.GeneratedAt.UTC(), Contributors: map[string]int{}}
	for _, d := range in.Sessions {
		s := d.Session
		r.Sessions = append(r.Sessions, Session{ID: s.ID, Agent: s.Agent, Version: s.AgentVersion, Models: s.Models, Fidelity: string(s.Fidelity),
			Steps: len(d.Steps), Duration: duration(s), Status: s.Status})
		for _, it := range d.Intent {
			r.Intent = append(r.Intent, Intent{Text: it.Text, Status: it.Status})
		}
	}
	if len(r.Intent) == 0 {
		prompts := 0
		for _, d := range in.Sessions {
			for _, s := range d.Steps {
				if s.Kind == model.StepUserMessage {
					prompts++
				}
			}
		}
		r.IntentNote = fmt.Sprintf("LLM analysis not run: %d prompts recorded, no intent items. Run `xis analyze`.", prompts)
	}
	r.FilesChanged = len(in.Files)
	for _, f := range in.Files {
		r.LinesAdded += f.Added
		r.LinesRemoved += f.Removed
	}
	for _, h := range in.Hunks {
		r.Contributors[attribution.Contributor(in.Provenance[h.ID], in.ActorKind)]++
	}
	for _, f := range in.Flags {
		loc := f.TargetKind + " " + short(f.TargetID)
		if in.Location != nil {
			loc = in.Location(f)
		}
		rf := Flag{ID: f.ID, Severity: string(f.Severity), Rule: f.RuleID, Location: loc, Message: f.Message, Reason: f.SuppressReason}
		if f.Suppressed {
			r.Suppressed = append(r.Suppressed, rf)
		} else {
			r.Flags = append(r.Flags, rf)
		}
	}
	byUnit := map[string][]model.Hunk{}
	for _, h := range in.Hunks {
		byUnit[h.ReviewUnitID] = append(byUnit[h.ReviewUnitID], h)
	}
	for _, u := range in.Units {
		hs := byUnit[u.ID]
		ru := Unit{Title: u.Title, Status: u.Status, Source: u.Source, Hunks: len(hs)}
		files := map[string]bool{}
		steps := map[int]bool{}
		for _, h := range hs {
			if !files[h.FilePath] {
				files[h.FilePath] = true
				ru.Files = append(ru.Files, h.FilePath)
			}
			if rat, ok := in.Rationales[h.ID]; ok && rat.Explanation != "" && !strings.HasPrefix(rat.Explanation, "No evidence") && len(ru.Explanation) < 3 {
				ru.Explanation = append(ru.Explanation, firstSentence(rat.Explanation))
			}
			for _, p := range in.Provenance[h.ID] {
				for _, d := range in.Sessions {
					if s, ok := d.Step(p.StepID); ok {
						steps[s.Seq] = true
					}
				}
			}
		}
		if len(ru.Explanation) == 0 {
			ru.Explanation = []string{provenanceSummary(hs, steps, in)}
		}
		r.Units = append(r.Units, ru)
	}
	for _, d := range in.Sessions {
		for _, tr := range d.TestRuns {
			s, _ := d.Step(tr.StepID)
			r.TestRuns = append(r.TestRuns, TestRun{Command: tr.Command, Outcome: tr.Outcome, Step: s.Seq})
		}
	}
	r.LastTestRun = "none"
	if n := len(r.TestRuns); n > 0 {
		r.LastTestRun = r.TestRuns[n-1].Outcome
	}
	hunkFile := map[string]string{}
	for _, h := range in.Hunks {
		hunkFile[h.ID] = h.FilePath
	}
	ids := make([]string, 0, len(in.Rationales))
	for id := range in.Rationales {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rat := in.Rationales[id]
		if rat.AgentClaim != "" {
			r.Claims = append(r.Claims, Claim{Quote: clip(rat.AgentClaim, 160), File: hunkFile[id], Verified: rat.Verified})
		}
	}
	return r
}

func provenanceSummary(hs []model.Hunk, steps map[int]bool, in Input) string {
	var seqs []int
	for s := range steps {
		seqs = append(seqs, s)
	}
	sort.Ints(seqs)
	contrib := map[string]int{}
	for _, h := range hs {
		contrib[attribution.Contributor(in.Provenance[h.ID], in.ActorKind)]++
	}
	var parts []string
	for _, k := range []string{"ai", "human", "mixed", "unknown"} {
		if contrib[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", contrib[k], k))
		}
	}
	if len(seqs) == 0 {
		return fmt.Sprintf("%s, no captured step (%s).", plural(len(hs), "hunk", "hunks"), strings.Join(parts, ", "))
	}
	var ss []string
	for i, s := range seqs {
		if i == 6 {
			ss = append(ss, "…")
			break
		}
		ss = append(ss, fmt.Sprintf("#%d", s))
	}
	return fmt.Sprintf("%s from steps %s (%s).", plural(len(hs), "hunk", "hunks"), strings.Join(ss, ", "), strings.Join(parts, ", "))
}

func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return clip(s, 240)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

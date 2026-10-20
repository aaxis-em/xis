package tui

import (
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

type Unit struct {
	model.ReviewUnit
	Hunks []model.Hunk
}

type TimelineStep struct {
	Step      model.Step
	Depth     int
	Actor     string
	Tool      string
	Files     []string
	Test      string
	Survive   int
	Abandoned bool
}

type Data struct {
	Title       string
	Agent       string
	Fidelity    string
	Status      string
	Steps       int
	Files       int
	Units       []Unit
	Flags       []model.Flag
	Sessions    map[string]*store.SessionData
	Provenance  map[string][]model.HunkProvenance
	Rationales  map[string]model.Rationale
	StepSession map[string]string
	ActorKind   map[string]string
	Intent      []model.IntentItem
	Timeline    []TimelineStep
	LLMOn       bool
	RepoTop     string
}

type Callbacks struct {
	SetUnitStatus func(unitID, status string) error
	// Ask is nil when no LLM is configured.
	Ask func(question string) (string, error)
}

func (d *Data) hunkByID(id string) (int, int, bool) {
	for ui, u := range d.Units {
		for hi, h := range u.Hunks {
			if h.ID == id {
				return ui, hi, true
			}
		}
	}
	return 0, 0, false
}

func (d *Data) hunkByFile(path string) (int, int, bool) {
	for ui, u := range d.Units {
		for hi, h := range u.Hunks {
			if h.FilePath == path {
				return ui, hi, true
			}
		}
	}
	return 0, 0, false
}

func (d *Data) step(id string) (model.Step, *store.SessionData, bool) {
	sd := d.Sessions[d.StepSession[id]]
	if sd == nil {
		for _, s := range d.Sessions {
			if st, ok := s.Step(id); ok {
				return st, s, true
			}
		}
		return model.Step{}, nil, false
	}
	st, ok := sd.Step(id)
	return st, sd, ok
}

func (d *Data) flagsForHunk(h model.Hunk) []model.Flag {
	var out []model.Flag
	for _, f := range d.Flags {
		if (f.TargetKind == "hunk" && f.TargetID == h.ID) || (f.TargetKind == "file" && f.TargetID == h.FilePath) {
			out = append(out, f)
		}
	}
	return out
}

func (d *Data) flagCount(sev model.Severity) int {
	n := 0
	for _, f := range d.Flags {
		if !f.Suppressed && f.Severity == sev {
			n++
		}
	}
	return n
}

func (d *Data) unitFlags(u Unit) int {
	n := 0
	for _, h := range u.Hunks {
		for _, f := range d.flagsForHunk(h) {
			if !f.Suppressed {
				n++
			}
		}
	}
	return n
}

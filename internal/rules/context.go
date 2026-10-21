package rules

import (
	"strings"

	"github.com/aaxis-em/xis/internal/adapters"
	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/redact"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/testdetect"
)

type Context struct {
	Change      model.Change
	Hunks       []model.Hunk
	Files       []gitx.FilePatch
	Sessions    []*store.SessionData
	Provenance  map[string][]model.HunkProvenance
	StepSession map[string]string
	ActorKind   map[string]string
	RawPatches  map[string]string
	RepoTop     string
	Cfg         config.Config
	Tests       *testdetect.Detector
	Redactor    *redact.Redactor
}

func NewContext(res *attribution.Result, sessions []*store.SessionData, repoTop string, cfg config.Config, red *redact.Redactor) *Context {
	c := &Context{
		Change: res.Change, Hunks: res.Hunks, Files: res.Files, Sessions: sessions,
		Provenance: map[string][]model.HunkProvenance{}, StepSession: res.StepSession, ActorKind: res.ActorKind,
		RawPatches: res.RawPatches, RepoTop: repoTop, Cfg: cfg, Redactor: red,
		Tests: testdetect.New(cfg.Capture.TestGlobs, cfg.Capture.TestCommands),
	}
	for _, p := range res.Provenance {
		c.Provenance[p.HunkID] = append(c.Provenance[p.HunkID], p)
	}
	return c
}

func (c *Context) patch(h model.Hunk) string {
	if raw, ok := c.RawPatches[h.ID]; ok {
		return raw
	}
	return h.Patch
}

func (c *Context) Lines(h model.Hunk, kind byte) []string {
	var out []string
	for i, l := range strings.Split(c.patch(h), "\n") {
		if i == 0 && strings.HasPrefix(l, "@@") {
			continue
		}
		if len(l) > 0 && l[0] == kind {
			out = append(out, l[1:])
		}
	}
	return out
}

func (c *Context) AgentLines(h model.Hunk) int {
	n := 0
	for _, p := range c.Provenance[h.ID] {
		if p.StepID != model.Unattributed && c.ActorKind[p.StepID] != "human" {
			n += max(p.Lines, 1)
		}
	}
	return n
}

func (c *Context) AgentSteps(h model.Hunk) []string {
	var out []string
	for _, p := range c.Provenance[h.ID] {
		if p.StepID != model.Unattributed && c.ActorKind[p.StepID] != "human" {
			out = append(out, p.StepID)
		}
	}
	return out
}

func (c *Context) SessionOfHunk(h model.Hunk) string {
	for _, p := range c.Provenance[h.ID] {
		if s := c.StepSession[p.StepID]; s != "" {
			return s
		}
	}
	if len(c.Change.SessionIDs) > 0 {
		return c.Change.SessionIDs[0]
	}
	return ""
}

func (c *Context) FilePatch(path string) (gitx.FilePatch, bool) {
	for _, f := range c.Files {
		if f.Path == path {
			return f, true
		}
	}
	return gitx.FilePatch{}, false
}

func (c *Context) HunksOf(path string) []model.Hunk {
	var out []model.Hunk
	for _, h := range c.Hunks {
		if h.FilePath == path {
			out = append(out, h)
		}
	}
	return out
}

func (c *Context) FileAgentTouched(path string) bool {
	for _, h := range c.HunksOf(path) {
		if c.AgentLines(h) > 0 {
			return true
		}
	}
	return false
}

func (c *Context) IsSourceFile(path string) bool {
	if c.Tests.IsTestFile(path) || testdetect.IsDocs(path) || testdetect.Classify(path) != testdetect.NotManifest {
		return false
	}
	return !testdetect.IsGenerated(path, "")
}

// RelPath turns an absolute tool path into a repository path.
func (c *Context) RelPath(p string) string {
	p = strings.TrimSpace(p)
	if c.RepoTop != "" && strings.HasPrefix(p, c.RepoTop+"/") {
		return strings.TrimPrefix(p, c.RepoTop+"/")
	}
	return strings.TrimPrefix(p, "./")
}

func (c *Context) agentStep(d *store.SessionData, s model.Step) bool {
	k := d.Actors[s.ActorID].Kind
	return k == "agent" || k == "subagent"
}

func (c *Context) command(d *store.SessionData, stepID string) string {
	tc, ok := d.ToolCalls[stepID]
	if !ok {
		return ""
	}
	return adapters.Command(tc.Input)
}

func testFailed(r model.TestRun) bool {
	return r.Outcome == model.OutcomeFail || r.Outcome == model.OutcomeError
}

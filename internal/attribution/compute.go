package attribution

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/redact"
	"github.com/aaxis-em/xis/internal/snapshot"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/testdetect"
)

var zeroTime = time.Unix(0, 0)

const (
	ModeSession = "session"
	ModeStaged  = "staged"
	ModeRange   = "range"
	ModePR      = "pr"
)

type Options struct {
	Mode      string
	SessionID string
	Range     string
}

type Engine struct {
	Redact func(string) string
	Cfg    config.Config
	Store  *store.Store
	Repo   snapshot.Repo
	Shadow *snapshot.Shadow
	Tests  *testdetect.Detector
}

func NewEngine(p config.Paths, cfg config.Config, st *store.Store, repoDir string) (*Engine, error) {
	r, err := snapshot.Discover(repoDir)
	if err != nil {
		return nil, err
	}
	sh, err := snapshot.Open(p.Shadow, r, cfg.Capture.Exclude)
	if err != nil {
		return nil, err
	}
	red, err := redact.New(cfg.Redact)
	if err != nil {
		return nil, err
	}
	return &Engine{Redact: red.String, Cfg: cfg, Store: st, Repo: r, Shadow: sh, Tests: testdetect.New(cfg.Capture.TestGlobs, cfg.Capture.TestCommands)}, nil
}

type Result struct {
	Change       model.Change
	Sessions     []model.Session
	Hunks        []model.Hunk
	Provenance   []model.HunkProvenance
	Units        []model.ReviewUnit
	Files        []gitx.FilePatch
	AttribCommit string
	BaseCommit   string
	StepSession  map[string]string
	ActorKind    map[string]string
	LineSteps    map[string]map[int]string
	// RawPatches holds the unredacted hunk patches in memory only, for the secret rule.
	RawPatches map[string]string
}

var ErrNoSession = errors.New("no session found for this repository")

// ResolveSession finds a session by xis ID, agent session ID or a prefix of
// either. An empty id means the session of the repository with the latest
// activity that has a prompt; a session that was opened and closed without
// one is skipped. A long session that started first can be the latest one.
func ResolveSession(st *store.Store, repoID, id string) (model.Session, error) {
	sessions, err := st.ListSessions(repoID, 0)
	if err != nil {
		return model.Session{}, err
	}
	if id == "" {
		if len(sessions) == 0 {
			return model.Session{}, ErrNoSession
		}
		active := func(s model.Session) time.Time {
			if s.EndedAt.After(s.StartedAt) {
				return s.EndedAt
			}
			return s.StartedAt
		}
		sort.SliceStable(sessions, func(i, j int) bool { return active(sessions[i]).After(active(sessions[j])) })
		for _, s := range sessions {
			if ok, err := st.HasPrompt(s.ID); err != nil {
				return model.Session{}, err
			} else if ok {
				return s, nil
			}
		}
		return sessions[0], nil
	}
	var found []model.Session
	for _, s := range sessions {
		if s.ID == id || s.AgentSessionID == id {
			return s, nil
		}
		if strings.HasPrefix(s.ID, strings.ToUpper(id)) || strings.HasPrefix(s.AgentSessionID, id) {
			found = append(found, s)
		}
	}
	switch len(found) {
	case 0:
		return model.Session{}, fmt.Errorf("session %q not found", id)
	case 1:
		return found[0], nil
	}
	return model.Session{}, fmt.Errorf("session prefix %q is ambiguous (%d sessions)", id, len(found))
}

func (e *Engine) resolveCommit(ref string) (string, error) {
	return e.Repo.Git().Run("rev-parse", "--verify", "-q", ref+"^{commit}")
}

func (e *Engine) defaultBranch() string {
	if out, err := e.Repo.Git().Run("symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return out
	}
	for _, b := range []string{"main", "master", "origin/main", "origin/master"} {
		if _, err := e.resolveCommit(b); err == nil {
			return b
		}
	}
	return ""
}

func (e *Engine) sessionsInRange(base, target string) ([]model.Session, error) {
	all, err := e.Store.ListSessions(e.Repo.ID, 0)
	if err != nil {
		return nil, err
	}
	commits := map[string]bool{base: true}
	if target != "" {
		out, err := e.Repo.Git().Run("rev-list", base+".."+target)
		if err == nil {
			for _, c := range strings.Fields(out) {
				commits[c] = true
			}
		}
	}
	var res []model.Session
	for _, s := range all {
		if s.ParentSessionID == "" && commits[s.BaseCommit] {
			res = append(res, s)
		}
	}
	return res, nil
}

// Compute builds the change for opts, attributes its hunks and stores it.
func (e *Engine) Compute(opts Options) (*Result, error) {
	res := &Result{StepSession: map[string]string{}, ActorKind: map[string]string{}, LineSteps: map[string]map[int]string{}, RawPatches: map[string]string{}}
	var base, targetRef, targetTree string
	var err error
	switch opts.Mode {
	case "", ModeSession:
		s, err := ResolveSession(e.Store, e.Repo.ID, opts.SessionID)
		if err != nil {
			return nil, err
		}
		res.Sessions = []model.Session{s}
		base = s.BaseCommit
		targetRef = "worktree"
	case ModeStaged:
		head := e.Repo.Head()
		base = head
		targetRef = "index"
		if res.Sessions, err = e.sessionsInRange(head, ""); err != nil {
			return nil, err
		}
	case ModeRange, ModePR:
		var a, b string
		if opts.Mode == ModeRange {
			var ok bool
			a, b, ok = strings.Cut(opts.Range, "..")
			if !ok || a == "" {
				return nil, fmt.Errorf("range %q: want A..B", opts.Range)
			}
			if b == "" {
				b = "HEAD"
			}
		} else {
			def := e.defaultBranch()
			if def == "" {
				return nil, fmt.Errorf("no default branch found (origin/HEAD, main or master)")
			}
			mb, err := e.Repo.Git().Run("merge-base", def, "HEAD")
			if err != nil {
				return nil, fmt.Errorf("merge-base %s HEAD: %w", def, err)
			}
			a, b = mb, "HEAD"
		}
		if base, err = e.resolveCommit(a); err != nil {
			return nil, fmt.Errorf("unknown revision %q", a)
		}
		bc, err := e.resolveCommit(b)
		if err != nil {
			return nil, fmt.Errorf("unknown revision %q", b)
		}
		targetRef = bc
		if res.Sessions, err = e.sessionsInRange(base, bc); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown change mode %q", opts.Mode)
	}
	sort.Slice(res.Sessions, func(i, j int) bool { return res.Sessions[i].StartedAt.Before(res.Sessions[j].StartedAt) })

	switch targetRef {
	case "worktree":
		targetTree, err = e.Shadow.WriteTree("review")
	case "index":
		idx := os.Getenv("GIT_INDEX_FILE")
		if idx == "" {
			idx = filepath.Join(e.Repo.GitDir, "index")
		} else if !filepath.IsAbs(idx) {
			idx = filepath.Join(e.Repo.Top, idx)
		}
		targetTree, err = e.Shadow.WriteIndexTree(idx)
	default:
		targetTree, err = e.Shadow.TreeOf(targetRef)
	}
	if err != nil {
		return nil, fmt.Errorf("target tree: %w", err)
	}
	if base == "" {
		if base, err = e.Shadow.EmptyTree(); err != nil {
			return nil, err
		}
	}
	res.BaseCommit = base

	var parents []string
	snapStep := map[string]string{}
	preSession := map[string]bool{}
	for _, s := range res.Sessions {
		if s.FinalSnapshot != "" && e.commitExists(s.FinalSnapshot) {
			parents = append(parents, s.FinalSnapshot)
		}
		steps, err := e.Store.ListSteps(s.ID)
		if err != nil {
			return nil, err
		}
		actors, err := e.Store.ListActors(s.ID)
		if err != nil {
			return nil, err
		}
		kinds := map[string]string{}
		for _, a := range actors {
			kinds[a.ID] = a.Kind
		}
		for _, st := range steps {
			res.StepSession[st.ID] = s.ID
			res.ActorKind[st.ID] = kinds[st.ActorID]
			if st.Snapshot != "" {
				snapStep[st.Snapshot] = st.ID
			}
			if st.Kind == model.StepHumanEdit && st.Snapshot != "" && st.Snapshot == s.BaseSnapshot {
				preSession[st.ID] = true
			}
		}
	}
	if len(parents) == 0 && e.commitExists(base) {
		parents = append(parents, base)
	}
	if res.AttribCommit, err = e.Shadow.CommitTree(targetTree, "xis attribution", parents...); err != nil {
		return nil, err
	}

	sessionIDs := make([]string, len(res.Sessions))
	for i, s := range res.Sessions {
		sessionIDs[i] = s.ID
	}
	res.Change = model.Change{
		ID:         model.StableID(zeroTime, e.Repo.ID, base, targetRef, strings.Join(sessionIDs, ",")),
		RepoID:     e.Repo.ID,
		BaseRef:    base,
		TargetRef:  targetRef,
		SessionIDs: sessionIDs,
		CreatedAt:  time.Now().UTC(),
	}

	out, err := e.Shadow.Git().Output("diff", "-M", "-U3", "--no-color", "--no-ext-diff", base, res.AttribCommit)
	if err != nil {
		return nil, err
	}
	if res.Files, err = gitx.ParsePatch(out); err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}
	if err := e.buildHunks(res, snapStep, preSession); err != nil {
		return nil, err
	}
	res.Units = GroupDeterministic(res.Change.ID, res.Hunks)
	e.reuseLLMUnits(res)
	return res, e.save(res)
}

func (e *Engine) commitExists(c string) bool {
	_, err := e.Shadow.Git().Run("cat-file", "-e", c+"^{commit}")
	return err == nil
}

// buildHunks attributes each added line. A line that blame gives to the
// edits before the session counts as human only if that edit added it; the
// first snapshot also holds uncommitted lines of earlier sessions.
func (e *Engine) buildHunks(res *Result, snapStep map[string]string, preSession map[string]bool) error {
	edits := map[string][]editRef{}
	imported := map[string][]editRef{}
	live := map[string][]editRef{}
	for _, s := range res.Sessions {
		es, err := e.Store.ListEdits(s.ID)
		if err != nil {
			return err
		}
		for _, ed := range es {
			edits[ed.FilePath] = append(edits[ed.FilePath], editRef{ed})
			if s.Fidelity == model.FidelityImported {
				imported[ed.FilePath] = append(imported[ed.FilePath], editRef{ed})
			} else {
				live[ed.FilePath] = append(live[ed.FilePath], editRef{ed})
			}
		}
	}
	for _, fp := range res.Files {
		var blame, plain map[int]string
		hasAdded := false
		for _, h := range fp.Hunks {
			for _, l := range gitx.HunkLines(h) {
				if l.Kind == '+' {
					hasAdded = true
				}
			}
		}
		if hasAdded && !fp.Binary {
			if b, err := e.blame(res.AttribCommit, fp.Path, true); err == nil {
				blame = b
			}
		}
		lineSteps := map[int]string{}
		res.LineSteps[fp.Path] = lineSteps
		if len(fp.Hunks) == 0 {
			h := model.Hunk{ChangeID: res.Change.ID, FilePath: fp.Path, Patch: strings.Join(fp.Extended, "\n")}
			h.ID = model.StableID(zeroTime, res.Change.ID, fp.Path, "file")
			h.ChangeType = ClassifyHunk(e.Tests, fp, nil)
			res.Hunks = append(res.Hunks, h)
			if step := lastEditStep(live[fp.Path], res.StepSession); step != "" {
				res.Provenance = append(res.Provenance, model.HunkProvenance{HunkID: h.ID, StepID: step, Lines: 0})
			} else {
				res.Provenance = append(res.Provenance, model.HunkProvenance{HunkID: h.ID, StepID: model.Unattributed, Lines: 0})
			}
			continue
		}
		for _, dh := range fp.Hunks {
			lines := gitx.HunkLines(dh)
			var added []string
			for _, l := range lines {
				if l.Kind == '+' {
					added = append(added, l.Text)
				}
			}
			h := model.Hunk{
				ChangeID: res.Change.ID, FilePath: fp.Path,
				OldStart: int(dh.OrigStartLine), OldLines: int(dh.OrigLines),
				NewStart: int(dh.NewStartLine), NewLines: int(dh.NewLines),
				Patch:       gitx.HunkHeader(dh) + "\n" + string(dh.Body),
				ContentHash: ContentHash(added),
			}
			h.ID = model.StableID(zeroTime, res.Change.ID, fp.Path, strconv.Itoa(h.OldStart), strconv.Itoa(h.NewStart), h.ContentHash)
			h.ChangeType = ClassifyHunk(e.Tests, fp, lines)
			counts := map[string]int{}
			for _, l := range lines {
				switch l.Kind {
				case '+':
					step := model.Unattributed
					if blame != nil {
						s, ok := snapStep[blame[l.NewLine]]
						if !ok {
							if plain == nil {
								plain, _ = e.blame(res.AttribCommit, fp.Path, false)
							}
							s, ok = snapStep[plain[l.NewLine]]
						}
						if ok {
							step = s
						}
						if preSession[step] && addedBy(edits[fp.Path], l.Text, map[string]string{step: ""}) != step {
							step = model.Unattributed
						}
					} else if s := lastEditStep(live[fp.Path], res.StepSession); s != "" {
						step = s
					}
					if step == model.Unattributed {
						step = addedBy(imported[fp.Path], l.Text, res.StepSession)
					}
					lineSteps[l.NewLine] = step
					counts[step]++
				case '-':
					step := removedBy(edits[fp.Path], l.Text, res.StepSession)
					if step == "" {
						step = model.Unattributed
					}
					counts[step]++
				}
			}
			ids := make([]string, 0, len(counts))
			for id := range counts {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				res.Provenance = append(res.Provenance, model.HunkProvenance{HunkID: h.ID, StepID: id, Lines: counts[id]})
			}
			res.RawPatches[h.ID] = h.Patch
			if e.Redact != nil {
				h.Patch = e.Redact(h.Patch)
			}
			res.Hunks = append(res.Hunks, h)
		}
	}
	return nil
}

type editRef struct{ model.Edit }

func lastEditStep(edits []editRef, known map[string]string) string {
	for i := len(edits) - 1; i >= 0; i-- {
		if _, ok := known[edits[i].StepID]; ok {
			return edits[i].StepID
		}
	}
	return ""
}

// removedBy returns the step whose edit first removed a line with this text.
// Edits are in chain order, so the first match is the step that made the
// line disappear.
func removedBy(edits []editRef, text string, known map[string]string) string {
	want := normalize(text)
	for _, ed := range edits {
		if _, ok := known[ed.StepID]; !ok {
			continue
		}
		for _, l := range strings.Split(ed.Patch, "\n") {
			if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") && normalize(l[1:]) == want {
				return ed.StepID
			}
		}
	}
	return ""
}

// blame with moves follows lines that a step moved, but it also gives a new
// line that copies an older one to the older commit. The caller then asks
// again without moves.
func (e *Engine) blame(commit, path string, moves bool) (map[int]string, error) {
	args := []string{"blame", "--porcelain", "-w"}
	if moves {
		args = append(args, "-M")
	}
	out, err := e.Shadow.Git().Output(append(args, commit, "--", path)...)
	if err != nil {
		return nil, err
	}
	return ParseBlame(out), nil
}

// ParseBlame maps each final line number to the commit that blame gives it.
func ParseBlame(out []byte) map[int]string {
	res := map[int]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "\t") {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 3 && len(f[0]) >= 40 && isHex(f[0]) {
			final, err := strconv.Atoi(f[2])
			if err != nil {
				continue
			}
			res[final] = f[0]
		}
	}
	return res
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func (e *Engine) save(res *Result) error {
	return e.Store.Tx(func(tx *store.Store) error {
		if err := tx.PutChange(res.Change); err != nil {
			return err
		}
		if err := tx.DeleteChangeHunks(res.Change.ID); err != nil {
			return err
		}
		for _, h := range res.Hunks {
			if err := tx.PutHunk(h); err != nil {
				return err
			}
		}
		for _, p := range res.Provenance {
			if err := tx.PutProvenance(p); err != nil {
				return err
			}
		}
		keep := make([]string, len(res.Units))
		for i, u := range res.Units {
			keep[i] = u.ID
			if err := tx.PutReviewUnit(u); err != nil {
				return err
			}
		}
		return tx.DeleteReviewUnitsExcept(res.Change.ID, keep)
	})
}

func (r *Result) ProvenanceOf(hunkID string) []model.HunkProvenance {
	var out []model.HunkProvenance
	for _, p := range r.Provenance {
		if p.HunkID == hunkID {
			out = append(out, p)
		}
	}
	return out
}

func (r *Result) HunkAt(file string, line int) (model.Hunk, bool) {
	for _, h := range r.Hunks {
		if h.FilePath != file {
			continue
		}
		end := h.NewStart + h.NewLines - 1
		if h.NewLines == 0 {
			end = h.NewStart
		}
		if line >= h.NewStart && line <= end {
			return h, true
		}
	}
	return model.Hunk{}, false
}

// reuseLLMUnits keeps a stored LLM grouping when the change still has exactly
// the same hunks; otherwise the fixed grouping replaces it.
func (e *Engine) reuseLLMUnits(res *Result) {
	units, err := e.Store.ListReviewUnits(res.Change.ID)
	if err != nil {
		return
	}
	llmUnits := map[string]model.ReviewUnit{}
	for _, u := range units {
		if u.Source == "llm" {
			llmUnits[u.ID] = u
		}
	}
	if len(llmUnits) == 0 {
		return
	}
	old, err := e.Store.ListHunks(res.Change.ID)
	if err != nil || len(old) != len(res.Hunks) {
		return
	}
	assign := map[string]string{}
	for _, h := range old {
		if _, ok := llmUnits[h.ReviewUnitID]; !ok {
			return
		}
		assign[h.ID] = h.ReviewUnitID
	}
	for _, h := range res.Hunks {
		if _, ok := assign[h.ID]; !ok {
			return
		}
	}
	for i := range res.Hunks {
		res.Hunks[i].ReviewUnitID = assign[res.Hunks[i].ID]
	}
	res.Units = res.Units[:0]
	for _, u := range units {
		if u.Source == "llm" {
			res.Units = append(res.Units, u)
		}
	}
}

// addedBy attributes a line of an imported session by its content: the last
// recorded edit that added a line with the same text. It never guesses: a
// line that no recorded edit added stays unattributed.
func addedBy(edits []editRef, text string, known map[string]string) string {
	want := normalize(text)
	for i := len(edits) - 1; i >= 0; i-- {
		ed := edits[i]
		if _, ok := known[ed.StepID]; !ok {
			continue
		}
		for _, l := range strings.Split(ed.Patch, "\n") {
			if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") && normalize(l[1:]) == want {
				return ed.StepID
			}
		}
	}
	return model.Unattributed
}

package rules

import (
	"maps"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/aaxis-em/xis/internal/adapters"
	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/model"
)

type Scope struct {
	Dirs  map[string]bool
	Files map[string]bool
	Names map[string]bool
	// FromPaths is false when only identifiers from the prompts formed the scope.
	FromPaths bool
	tests     func(string) bool
	stems     map[string]bool
	idents    []string
}

var (
	pathTokenRe  = regexp.MustCompile(`[A-Za-z0-9_.\-]+(/[A-Za-z0-9_.\-]+)+/?|[A-Za-z0-9_\-]+\.[A-Za-z][A-Za-z0-9]{0,5}\b`)
	backtickRe   = regexp.MustCompile("`([^`\\s]{3,})`")
	identifierRe = regexp.MustCompile(`\b([a-z]+[A-Z][A-Za-z0-9]*|[A-Z][a-z0-9]+[A-Z][A-Za-z0-9]*|[a-z0-9]+_[a-z0-9_]+)\b`)
	testMarkerRe = regexp.MustCompile(`(?i)(_test|\.test|\.spec|_spec|^test_|tests?$)`)
)

func (s *Scope) Empty() bool { return len(s.Dirs) == 0 && len(s.Files) == 0 && len(s.Names) == 0 }

func (s *Scope) addPath(p string) {
	p = strings.TrimPrefix(strings.Trim(p, "`'\".,:;()[]"), "./")
	if p == "" || strings.HasPrefix(p, "http") || strings.Contains(p, "://") {
		return
	}
	switch {
	case strings.HasSuffix(p, "/"):
		s.Dirs[strings.TrimSuffix(p, "/")] = true
	case strings.Contains(p, "/"):
		s.Files[p] = true
		if path.Ext(p) == "" {
			s.Dirs[p] = true
		}
	default:
		s.Names[strings.ToLower(p)] = true
	}
}

func (s *Scope) directMatch(p string) bool {
	if s.Files[p] || s.Names[strings.ToLower(path.Base(p))] || s.Names[stem(p)] {
		return true
	}
	for f := range s.Files {
		if strings.HasSuffix(p, "/"+f) {
			return true
		}
	}
	for d := range s.Dirs {
		if d == "." {
			if !strings.Contains(p, "/") {
				return true
			}
			continue
		}
		if p == d || strings.HasPrefix(p, d+"/") || strings.Contains(p, "/"+d+"/") {
			return true
		}
	}
	return false
}

// mirrorStem returns the stem of the source file that a test file tests,
// for example "calc" for calc_test.go, test_calc.py and calc.spec.ts.
func mirrorStem(p string) string {
	b := strings.ToLower(path.Base(p))
	if i := strings.Index(b, "."); i > 0 && !strings.HasPrefix(b[i:], ".test") && !strings.HasPrefix(b[i:], ".spec") {
		b = b[:i]
	}
	b = strings.TrimSuffix(strings.TrimSuffix(b, ".ts"), ".js")
	return testMarkerRe.ReplaceAllString(strings.SplitN(b, ".", 2)[0], "")
}

func (s *Scope) Contains(p string) bool {
	if s.directMatch(p) {
		return true
	}
	if s.tests != nil && s.tests(p) {
		return s.stems[mirrorStem(p)]
	}
	return false
}

func (s *Scope) Describe() string {
	var parts []string
	for d := range s.Dirs {
		parts = append(parts, d+"/")
	}
	for f := range s.Files {
		parts = append(parts, f)
	}
	for n := range s.Names {
		parts = append(parts, n)
	}
	sort.Strings(parts)
	if len(parts) > 6 {
		parts = append(parts[:6], "…")
	}
	return "scope: " + strings.Join(parts, ", ")
}

func (s *Scope) addRead(c *Context, p string, search bool) {
	rel := c.RelPath(p)
	if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "..") || strings.Contains(rel, "...") {
		return
	}
	dir := path.Dir(rel)
	if search && path.Ext(rel) == "" {
		dir = rel
	}
	s.Dirs[dir] = true
}

func newScope(c *Context) *Scope {
	s := &Scope{Dirs: map[string]bool{}, Files: map[string]bool{}, Names: map[string]bool{}, tests: c.Tests.IsTestFile, stems: map[string]bool{}}
	for _, p := range c.Cfg.Scope.Paths {
		p = strings.TrimSpace(p)
		if p != "" {
			s.Dirs[strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/")] = true
		}
	}
	return s
}

func (s *Scope) clone() *Scope {
	n := &Scope{Dirs: maps.Clone(s.Dirs), Files: maps.Clone(s.Files), Names: maps.Clone(s.Names), tests: s.tests, stems: map[string]bool{}}
	n.idents = slices.Clone(s.idents)
	return n
}

func (s *Scope) addPrompt(text string) {
	for _, m := range pathTokenRe.FindAllString(text, -1) {
		s.addPath(m)
	}
	for _, m := range backtickRe.FindAllStringSubmatch(text, -1) {
		s.addPath(m[1])
	}
	for _, m := range identifierRe.FindAllString(text, -1) {
		s.Names[strings.ToLower(m)] = true
		if id := normIdent(m); !slices.Contains(s.idents, id) {
			s.idents = append(s.idents, id)
		}
	}
}

func (s *Scope) addTool(c *Context, tc model.ToolCall) {
	switch tc.Category {
	case model.CatRead, model.CatSearch:
		for _, p := range claudecode.ToolPaths(tc.Input) {
			s.addRead(c, p, tc.Category == model.CatSearch)
		}
	case model.CatShell, model.CatTest:
		for _, p := range pathTokenRe.FindAllString(adapters.Command(tc.Input), -1) {
			s.addRead(c, p, false)
		}
	}
}

func (s *Scope) finish(c *Context) {
	s.FromPaths = len(s.Dirs) > 0 || len(s.Files) > 0
	for _, f := range c.Files {
		if !s.Files[f.Path] && usesIdent(c, f.Path, s.idents) {
			s.Files[f.Path] = true
		}
	}
	for _, f := range c.Files {
		if s.directMatch(f.Path) {
			s.stems[stem(f.Path)] = true
		}
	}
	for f := range s.Files {
		s.stems[stem(f)] = true
	}
	for n := range s.Names {
		s.stems[strings.SplitN(n, ".", 2)[0]] = true
	}
}

// TurnScopes gives each step the scope of its turn. The scope is conservative
// and grows over the session: the paths and identifiers in the prompts so
// far, the directories the agent read or named in a shell command before the
// first edit of each turn, the configured scope.paths, files whose added
// lines use a prompt identifier, and test files that mirror an in-scope file.
func TurnScopes(c *Context) map[string]*Scope {
	out := map[string]*Scope{}
	for _, d := range c.Sessions {
		cur := newScope(c)
		turns := []*Scope{cur}
		edited := false
		for _, st := range d.Steps {
			switch {
			case st.Kind == model.StepUserMessage:
				cur = cur.clone()
				cur.addPrompt(st.Text)
				turns = append(turns, cur)
				edited = false
			case edited:
			case len(d.EditsOf(st.ID)) > 0 && c.agentStep(d, st):
				edited = true
			default:
				if tc, ok := d.ToolCalls[st.ID]; ok {
					cur.addTool(c, tc)
				}
			}
			out[st.ID] = cur
		}
		for _, t := range turns {
			t.finish(c)
		}
	}
	return out
}

// normIdent makes expires_at, expiresAt and ExpiresAt equal.
func normIdent(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }

func usesIdent(c *Context, file string, idents []string) bool {
	if len(idents) == 0 {
		return false
	}
	text := c.addedIdentText(file)
	for _, id := range idents {
		if strings.Contains(text, id) {
			return true
		}
	}
	return false
}

// addedIdentText is the added lines of file through normIdent, one per
// line. Each turn of a session checks every file, so it is made once.
func (c *Context) addedIdentText(file string) string {
	if t, ok := c.addedIdents[file]; ok {
		return t
	}
	var b strings.Builder
	for _, h := range c.HunksOf(file) {
		for _, l := range c.Lines(h, '+') {
			b.WriteString(normIdent(l) + "\n")
		}
	}
	if c.addedIdents == nil {
		c.addedIdents = map[string]string{}
	}
	c.addedIdents[file] = b.String()
	return c.addedIdents[file]
}

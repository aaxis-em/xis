package rules

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/model"
)

type Scope struct {
	Dirs  map[string]bool
	Files map[string]bool
	Names map[string]bool
	tests func(string) bool
	stems map[string]bool
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

// InferScope is conservative: the scope is the paths and identifiers in the
// user prompts, the directories the agent read before its first edit, the
// configured scope.paths, and test files that mirror an in-scope file.
func InferScope(c *Context) *Scope {
	s := &Scope{Dirs: map[string]bool{}, Files: map[string]bool{}, Names: map[string]bool{}, tests: c.Tests.IsTestFile, stems: map[string]bool{}}
	for _, p := range c.Cfg.Scope.Paths {
		p = strings.TrimSpace(p)
		if p != "" {
			s.Dirs[strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/")] = true
		}
	}
	for _, d := range c.Sessions {
		firstEdit := len(d.Steps) + 1
		for _, st := range d.Steps {
			if len(d.EditsOf(st.ID)) > 0 && c.agentStep(d, st) {
				firstEdit = st.Seq
				break
			}
		}
		for _, st := range d.Steps {
			switch {
			case st.Kind == model.StepUserMessage:
				for _, m := range pathTokenRe.FindAllString(st.Text, -1) {
					s.addPath(m)
				}
				for _, m := range backtickRe.FindAllStringSubmatch(st.Text, -1) {
					s.addPath(m[1])
				}
				for _, m := range identifierRe.FindAllString(st.Text, -1) {
					s.Names[strings.ToLower(m)] = true
				}
			case st.Seq < firstEdit:
				tc, ok := d.ToolCalls[st.ID]
				if !ok || (tc.Category != model.CatRead && tc.Category != model.CatSearch) {
					continue
				}
				for _, p := range claudecode.ToolPaths(tc.Input) {
					rel := c.RelPath(p)
					if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "..") {
						continue
					}
					dir := path.Dir(rel)
					if tc.Category == model.CatSearch && path.Ext(rel) == "" {
						dir = rel
					}
					s.Dirs[dir] = true
				}
			}
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
	return s
}

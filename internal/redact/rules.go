package redact

import (
	_ "embed"
	"regexp"
	"regexp/syntax"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// shortcut: the secret rules are config/gitleaks.toml of gitleaks v8.30.1
// (gitleaks.LICENSE); copy a newer file over it to update them.
//
//go:embed gitleaks.toml
var gitleaksRules string

type allowlist struct {
	Condition   string   `toml:"condition"`
	RegexTarget string   `toml:"regexTarget"`
	Regexes     []string `toml:"regexes"`
	StopWords   []string `toml:"stopwords"`
	Paths       []string `toml:"paths"`
	res         []*regexp.Regexp
}

type secretRule struct {
	ID          string      `toml:"id"`
	Regex       string      `toml:"regex"`
	SecretGroup int         `toml:"secretGroup"`
	Entropy     float64     `toml:"entropy"`
	Keywords    []string    `toml:"keywords"`
	Path        string      `toml:"path"`
	Allowlists  []allowlist `toml:"allowlists"`
	re          *regexp.Regexp
	// local is true when each match holds a keyword of the rule.
	local bool
}

type ruleSet struct {
	Allowlist allowlist    `toml:"allowlist"`
	Rules     []secretRule `toml:"rules"`
}

var loadRules = sync.OnceValues(func() (*ruleSet, error) {
	var rs ruleSet
	if _, err := toml.Decode(gitleaksRules, &rs); err != nil {
		return nil, err
	}
	if err := rs.Allowlist.compile(); err != nil {
		return nil, err
	}
	var rules []secretRule
	for _, r := range rs.Rules {
		// Text has no file path, so a rule that needs one never matches, as in gitleaks.
		if r.Regex == "" || r.Path != "" && !regexp.MustCompile(r.Path).MatchString("") {
			continue
		}
		var err error
		if r.re, err = regexp.Compile(r.Regex); err != nil {
			return nil, err
		}
		// A match of [\s\S] or (?s) can run over many lines, past the window.
		multiline := strings.Contains(r.Regex, `[\s\S`) || strings.Contains(r.Regex, "(?s")
		if re, err := syntax.Parse(r.Regex, syntax.Perl); err == nil && !multiline {
			r.local = mustHold(re, r.Keywords)
		}
		for i := range r.Keywords {
			r.Keywords[i] = strings.ToLower(r.Keywords[i])
		}
		for i := range r.Allowlists {
			if err := r.Allowlists[i].compile(); err != nil {
				return nil, err
			}
		}
		rules = append(rules, r)
	}
	rs.Rules = rules
	return &rs, nil
})

func (a *allowlist) compile() error {
	for _, p := range a.Regexes {
		re, err := regexp.Compile(p)
		if err != nil {
			return err
		}
		a.res = append(a.res, re)
	}
	for i := range a.StopWords {
		a.StopWords[i] = strings.ToLower(a.StopWords[i])
	}
	return nil
}

// allows applies a gitleaks allowlist to a finding. A path check never
// passes, because text has no path.
func (a *allowlist) allows(secret, match, line string) bool {
	target := secret
	switch a.RegexTarget {
	case "match":
		target = match
	case "line":
		target = line
	}
	regexHit := false
	for _, re := range a.res {
		regexHit = regexHit || target != "" && re.MatchString(target)
	}
	stopHit := false
	lower := strings.ToLower(secret)
	for _, w := range a.StopWords {
		stopHit = stopHit || secret != "" && strings.Contains(lower, w)
	}
	if strings.EqualFold(a.Condition, "AND") {
		return len(a.Paths) == 0 && (len(a.res) == 0 || regexHit) && (len(a.StopWords) == 0 || stopHit)
	}
	return regexHit || stopHit
}

type located struct {
	Finding
	line int
}

func (rs *ruleSet) find(s string) []Finding {
	lower := strings.ToLower(s)
	var found []located
	for i := range rs.Rules {
		r := &rs.Rules[i]
		var locs [][]int
		if !r.local || len(s) < nearKeywordsMin || len(lower) != len(s) {
			if len(r.Keywords) > 0 && !containsAny(lower, r.Keywords) {
				continue
			}
			locs = r.re.FindAllStringIndex(s, -1)
		} else {
			locs = findNearKeywords(r, s, lower)
		}
		for _, loc := range locs {
			match := strings.Trim(s[loc[0]:loc[1]], "\n")
			line := lineAround(s, loc[0], loc[0]+len(match))
			if strings.Contains(line, "gitleaks:allow") {
				continue
			}
			secret := match
			if g := r.re.FindStringSubmatch(match); len(g) >= 2 {
				if r.SecretGroup > 0 {
					if len(g) <= r.SecretGroup {
						continue
					}
					secret = g[r.SecretGroup]
				} else {
					for _, v := range g[1:] {
						if v != "" {
							secret = v
							break
						}
					}
				}
			}
			if r.Entropy != 0 && shannon(secret) <= r.Entropy || rs.Allowlist.allows(secret, match, line) || allowedByAny(r.Allowlists, secret, match, line) {
				continue
			}
			if secret != "" {
				found = append(found, located{Finding{Rule: r.ID, Secret: secret}, strings.Count(s[:loc[0]], "\n")})
			}
		}
	}
	return dropGeneric(found)
}

// dropGeneric removes a generic finding when a specific rule found a secret
// that contains it on the same line, as gitleaks does.
func dropGeneric(found []located) []Finding {
	var out []Finding
	for _, f := range found {
		keep := true
		if strings.Contains(strings.ToLower(f.Rule), "generic") {
			for _, o := range found {
				if o.line == f.line && o.Rule != f.Rule && strings.Contains(o.Secret, f.Secret) && !strings.Contains(strings.ToLower(o.Rule), "generic") {
					keep = false
					break
				}
			}
		}
		if keep {
			out = append(out, f.Finding)
		}
	}
	return out
}

// keywordWindow is the number of lines around a keyword that a match of a
// local rule can reach. Texts shorter than nearKeywordsMin are scanned
// whole, as finding each keyword costs more there than it saves.
const (
	keywordWindow   = 2
	nearKeywordsMin = 8 << 10
)

// findNearKeywords runs a local rule only on the lines around its keywords,
// not on the whole text. In a long text that names many keywords, for
// example a config of secret rules, this is the difference between seconds
// and milliseconds.
func findNearKeywords(r *secretRule, s, lower string) [][]int {
	var starts []int
	for _, k := range r.Keywords {
		for off := 0; ; {
			i := strings.Index(lower[off:], k)
			if i < 0 {
				break
			}
			from := strings.LastIndexByte(s[:off+i], '\n')
			if from > 0 {
				from = strings.LastIndexByte(s[:from], '\n')
			}
			starts = append(starts, from+1)
			off += i + len(k)
		}
	}
	if len(starts) == 0 {
		return nil
	}
	sort.Ints(starts)
	var out [][]int
	end := -1
	for _, from := range starts {
		if from < end {
			continue
		}
		end = from
		for n := 0; n <= keywordWindow+1 && end < len(s); n++ {
			if j := strings.IndexByte(s[end:], '\n'); j >= 0 {
				end += j + 1
			} else {
				end = len(s)
			}
		}
		for _, loc := range r.re.FindAllStringIndex(s[from:end], -1) {
			if len(out) > 0 && from+loc[0] < out[len(out)-1][1] {
				continue
			}
			out = append(out, []int{from + loc[0], from + loc[1]})
		}
	}
	return out
}

// mustHold reports whether every match of re holds one of the keywords.
func mustHold(re *syntax.Regexp, keywords []string) bool {
	holds := func(strs []string) bool {
		for _, str := range strs {
			if !containsAny(str, keywords) {
				return false
			}
		}
		return len(strs) > 0
	}
	if strs, ok := finite(re); ok {
		return holds(strs)
	}
	switch re.Op {
	case syntax.OpConcat:
		// A run of parts with few matches, such as A(?:KIA|SIA), is checked
		// as the words it can match, because the parser splits them.
		run := []string{""}
		for _, sub := range re.Sub {
			strs, ok := finite(sub)
			if ok {
				if run = product(run, strs); run != nil {
					continue
				}
			} else if mustHold(sub, keywords) {
				return true
			}
			if holds(run) {
				return true
			}
			run = []string{""}
		}
		return holds(run)
	case syntax.OpAlternate:
		for _, sub := range re.Sub {
			if !mustHold(sub, keywords) {
				return false
			}
		}
		return len(re.Sub) > 0
	case syntax.OpCapture, syntax.OpPlus:
		return mustHold(re.Sub[0], keywords)
	case syntax.OpRepeat:
		return re.Min > 0 && mustHold(re.Sub[0], keywords)
	}
	return false
}

// finite returns the lower-case strings that re can match, if they are few.
func finite(re *syntax.Regexp) ([]string, bool) {
	switch re.Op {
	case syntax.OpLiteral:
		return []string{strings.ToLower(string(re.Rune))}, true
	case syntax.OpEmptyMatch:
		return []string{""}, true
	case syntax.OpCharClass:
		var out []string
		for i := 0; i < len(re.Rune); i += 2 {
			for c := re.Rune[i]; c <= re.Rune[i+1]; c++ {
				if len(out) == 8 {
					return nil, false
				}
				if l := strings.ToLower(string(c)); !slices.Contains(out, l) {
					out = append(out, l)
				}
			}
		}
		return out, true
	case syntax.OpCapture:
		return finite(re.Sub[0])
	case syntax.OpQuest:
		strs, ok := finite(re.Sub[0])
		return append(strs, ""), ok
	case syntax.OpAlternate, syntax.OpConcat:
		out := []string{""}
		if re.Op == syntax.OpAlternate {
			out = nil
		}
		for _, sub := range re.Sub {
			strs, ok := finite(sub)
			if !ok {
				return nil, false
			}
			if re.Op == syntax.OpAlternate {
				out = append(out, strs...)
			} else if out = product(out, strs); out == nil {
				return nil, false
			}
		}
		return out, len(out) <= 64
	}
	return nil, false
}

func product(a, b []string) []string {
	if len(a)*len(b) > 64 {
		return nil
	}
	var out []string
	for _, x := range a {
		for _, y := range b {
			out = append(out, x+y)
		}
	}
	return out
}

func allowedByAny(lists []allowlist, secret, match, line string) bool {
	for i := range lists {
		if lists[i].allows(secret, match, line) {
			return true
		}
	}
	return false
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// lineAround returns the whole lines of s that hold s[start:end].
func lineAround(s string, start, end int) string {
	from := strings.LastIndexByte(s[:start], '\n') + 1
	to := strings.IndexByte(s[end:], '\n')
	if to < 0 {
		return s[from:]
	}
	return s[from : end+to]
}

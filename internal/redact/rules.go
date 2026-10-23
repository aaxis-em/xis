package redact

import (
	_ "embed"
	"regexp"
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
		if len(r.Keywords) > 0 && !containsAny(lower, r.Keywords) {
			continue
		}
		for _, loc := range r.re.FindAllStringIndex(s, -1) {
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

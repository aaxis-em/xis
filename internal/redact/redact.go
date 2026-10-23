package redact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/aaxis-em/xis/internal/config"
)

type Finding struct {
	Rule   string
	Secret string
}

type Redactor struct {
	rules    *ruleSet
	patterns []*regexp.Regexp
	entropy  float64
	minLen   int
}

func New(cfg config.Redact) (*Redactor, error) {
	rules, err := loadRules()
	if err != nil {
		return nil, fmt.Errorf("load secret rules: %w", err)
	}
	r := &Redactor{rules: rules, entropy: cfg.EntropyThreshold, minLen: cfg.EntropyMinLength}
	if r.minLen <= 0 {
		r.minLen = 20
	}
	for _, p := range cfg.Patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("redact.patterns %q: %w", p, err)
		}
		r.patterns = append(r.patterns, re)
	}
	return r, nil
}

var tokenRe = regexp.MustCompile(`[A-Za-z0-9+_\-=]{8,}`)

func shannon(s string) float64 {
	counts := map[rune]int{}
	for _, c := range s {
		counts[c]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range counts {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// isCharset reports an alphabet literal such as "abc...xyz0123456789". Half
// of its neighbours are consecutive characters; a random secret has almost none.
func isCharset(s string) bool {
	runs := 0
	for i := 1; i < len(s); i++ {
		if s[i] == s[i-1]+1 {
			runs++
		}
	}
	return runs*2 >= len(s)
}

func hasDigitAndLetter(s string) bool {
	d, l := false, false
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			d = true
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			l = true
		}
	}
	return d && l
}

func (r *Redactor) Findings(s string) []Finding {
	if s == "" {
		return nil
	}
	out := r.rules.find(s)
	for _, re := range r.patterns {
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			secret := m[0]
			if len(m) > 1 && m[1] != "" {
				secret = m[1]
			}
			out = append(out, Finding{Rule: "custom", Secret: secret})
		}
	}
	if r.entropy > 0 {
		for _, tok := range tokenRe.FindAllString(s, -1) {
			if len(tok) >= r.minLen && hasDigitAndLetter(tok) && !isCharset(tok) && shannon(tok) >= r.entropy {
				out = append(out, Finding{Rule: "entropy", Secret: tok})
			}
		}
	}
	return out
}

func Token(rule, secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("[REDACTED:%s:%s]", rule, hex.EncodeToString(sum[:])[:4])
}

func (r *Redactor) String(s string) string {
	fs := r.Findings(s)
	if len(fs) == 0 {
		return s
	}
	// Longest secrets first, so a secret that contains another is replaced whole.
	sort.SliceStable(fs, func(i, j int) bool { return len(fs[i].Secret) > len(fs[j].Secret) })
	for _, f := range fs {
		s = strings.ReplaceAll(s, f.Secret, Token(f.Rule, f.Secret))
	}
	return s
}

func (r *Redactor) JSON(raw []byte) []byte {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		b, _ := json.Marshal(r.String(string(raw)))
		return b
	}
	v = r.walk(v)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func (r *Redactor) walk(v any) any {
	switch t := v.(type) {
	case string:
		return r.String(t)
	case []any:
		for i := range t {
			t[i] = r.walk(t[i])
		}
		return t
	case map[string]any:
		for k, x := range t {
			t[k] = r.walk(x)
		}
		return t
	}
	return v
}

const TruncMarker = "\n[... truncated by xis: %d of %d bytes kept ...]"

func Truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && cut < len(s) && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + fmt.Sprintf(TruncMarker, cut, len(s))
}

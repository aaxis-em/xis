package redact

import (
	"strings"
	"testing"

	"github.com/aaxis-em/xis/internal/config"
)

func newR(t *testing.T) *Redactor {
	t.Helper()
	r, err := New(config.Default().Redact)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func secretParts(s string) []string {
	if strings.Contains(s, "PRIVATE KEY") {
		return []string{"MIIEowIBAAKCAQEA3Tz2mr7SZiAMfQyuvBjM9Oi8TjVxQsq3Lr2Kp9Wx4Yz7Nb6Hc1Jd5"}
	}
	return []string{s}
}

func TestFakeSecretsRedacted(t *testing.T) {
	r := newR(t)
	for name, secret := range FakeSecrets {
		in := "config:\n export KEY=" + secret + "\nend"
		out := r.String(in)
		for _, part := range secretParts(secret) {
			if strings.Contains(out, part) {
				t.Errorf("%s: secret survived: %s", name, out)
			}
		}
		if !strings.Contains(out, "[REDACTED:") {
			t.Errorf("%s: no token in %s", name, out)
		}
	}
}

func TestStableToken(t *testing.T) {
	r := newR(t)
	s := FakeSecrets["github"]
	a := r.String("a " + s)
	b := r.String("b " + s + " and " + s)
	tok := strings.TrimPrefix(a, "a ")
	if strings.Count(b, tok) != 2 {
		t.Errorf("token not stable: %q vs %q", a, b)
	}
}

func TestNoFalsePositives(t *testing.T) {
	r := newR(t)
	for _, s := range []string{
		"internal/adapters/claudecode/transcript_parser.go",
		"commit 3f786850e387550fdab836ed7e6dc881de23001b",
		"func TestEditAfterFailureRuleFiresWhenTestsFail(t *testing.T) {",
		"go test ./... ok github.com/aaxis-em/xis/internal/store 0.071s",
	} {
		if out := r.String(s); out != s {
			t.Errorf("changed %q to %q", s, out)
		}
	}
}

func TestCustomPatternAndJSON(t *testing.T) {
	cfg := config.Default().Redact
	cfg.Patterns = []string{`internal-token-(\w+)`}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out := string(r.JSON([]byte(`{"cmd":"curl -H 'X: internal-token-abc123'","n":3,"nested":["` + FakeSecrets["aws"] + `"]}`)))
	if strings.Contains(out, "abc123") || strings.Contains(out, FakeSecrets["aws"]) || !strings.Contains(out, `"n":3`) {
		t.Errorf("got %s", out)
	}
	if _, err := New(config.Redact{Patterns: []string{"("}}); err == nil {
		t.Error("want error for bad pattern")
	}
}

func TestTruncate(t *testing.T) {
	s := strings.Repeat("é", 100)
	out := Truncate(s, 51)
	if !strings.Contains(out, "truncated by xis") || strings.Contains(out, "�") {
		t.Errorf("got %q", out)
	}
	if Truncate("abc", 10) != "abc" {
		t.Error("short string changed")
	}
}

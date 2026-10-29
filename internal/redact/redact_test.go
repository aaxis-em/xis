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
		`alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"`,
		`const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"`,
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

// A long text that names many keywords goes the fast way; it must find the
// same secrets as a short one.
func TestLongTextFindsSecrets(t *testing.T) {
	r := newR(t)
	filler := strings.Repeat("token key secret api password auth credential sgp_ AKIA\n", 400)
	for name, secret := range FakeSecrets {
		out := r.String(filler + "config:\n export KEY=" + secret + "\nend\n" + filler)
		for _, part := range secretParts(secret) {
			if strings.Contains(out, part) {
				t.Errorf("%s: secret survived in a long text", name)
			}
		}
	}
}

func TestLongPrivateKeyInLongText(t *testing.T) {
	body := strings.Repeat("MIIEowIBAAKCAQEA3Tz2mr7SZiAMfQyuvBjM9Oi8TjVxQsq3Lr2Kp9Wx4Yz7Nb6Hc1Jd5\n", 25)
	key := "-----BEGIN RSA PRIVATE KEY-----\n" + body + "-----END RSA PRIVATE KEY-----"
	filler := strings.Repeat("token key secret\n", 600)
	rs, err := loadRules()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rs.find(filler + key + "\n" + filler) {
		if f.Rule == "private-key" {
			return
		}
	}
	t.Error("the private-key rule missed a 25-line key in a long text")
}

func TestLocalRules(t *testing.T) {
	rs, err := loadRules()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"github-pat": true, "generic-api-key": true, "sourcegraph-access-token": false, "private-key": false}
	for _, r := range rs.Rules {
		if w, ok := want[r.ID]; ok && r.local != w {
			t.Errorf("%s: local = %v, want %v", r.ID, r.local, w)
		}
	}
}

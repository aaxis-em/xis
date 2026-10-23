package textx

import (
	"testing"
	"unicode/utf8"
)

func TestOneLine(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"a  b\n\tc", 0, "a b c"},
		{"hello world", 5, "hell…"},
		{"नमस्ते दुनिया", 4, "नमस…"},
		{"short", 10, "short"},
	} {
		got := OneLine(c.in, c.n)
		if got != c.want || !utf8.ValidString(got) {
			t.Errorf("OneLine(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
	if FirstLine("a\nb") != "a" {
		t.Error("FirstLine")
	}
}

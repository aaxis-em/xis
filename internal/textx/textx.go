// Package textx shortens text for display on one line.
package textx

import "strings"

// OneLine joins the words of s with single spaces and cuts it to n runes
// with a trailing "…". n <= 0 keeps all of it.
func OneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if n > 0 && len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func FirstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

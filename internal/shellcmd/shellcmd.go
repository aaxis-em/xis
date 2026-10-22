// Package shellcmd separates what a shell command runs from the data in it,
// so that the rules do not take the text of a file or a message for a command.
package shellcmd

import (
	"regexp"
	"strings"
)

var (
	heredocRe = regexp.MustCompile(`(^|[^<])<<(-?)\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	// scriptRe matches the end of the text before a quoted script that the
	// shell runs: sh -c '...', eval "...", ssh host '...'.
	scriptRe   = regexp.MustCompile(`(\b(sh|bash|zsh|dash|ksh)(\s+-\w+)*\s+-\w*c|\beval|\bssh\b[^;&|\n]*)\s*$`)
	segmentSep = regexp.MustCompile(`&&|\|\||[;|\n]`)
)

// WithoutHeredocs removes the bodies of heredocs. A body is input data, for
// example the content of a file that the command writes.
func WithoutHeredocs(cmd string) string {
	type doc struct {
		word string
		tabs bool
	}
	var out []string
	var pending []doc
	for _, l := range strings.Split(cmd, "\n") {
		if len(pending) > 0 {
			end := l
			if pending[0].tabs {
				end = strings.TrimLeft(l, "\t")
			}
			if strings.TrimRight(end, " ") == pending[0].word {
				pending = pending[1:]
			}
			continue
		}
		out = append(out, l)
		for _, m := range heredocRe.FindAllStringSubmatch(l, -1) {
			pending = append(pending, doc{word: m[3], tabs: m[2] == "-"})
		}
	}
	return strings.Join(out, "\n")
}

// Code is WithoutHeredocs with the text inside quotes removed, except a
// script that sh -c, eval or ssh runs. The quotes stay, so `git commit -m
// "rm -rf"` becomes `git commit -m ""`.
func Code(cmd string) string {
	s := WithoutHeredocs(cmd)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			b.WriteString(s[i : i+2])
			i++
			continue
		case c != '\'' && c != '"':
			b.WriteByte(c)
			continue
		}
		end := closingQuote(s, i)
		keep := scriptRe.MatchString(b.String())
		b.WriteByte(c)
		if keep {
			b.WriteString(s[i+1 : end])
		}
		if end < len(s) {
			b.WriteByte(s[end])
		}
		i = end
	}
	return b.String()
}

func closingQuote(s string, open int) int {
	for j := open + 1; j < len(s); j++ {
		switch {
		case s[j] == '\\' && s[open] == '"':
			j++
		case s[j] == s[open]:
			return j
		}
	}
	return len(s)
}

// Segment returns the simple command around byte offset i of s, for
// example "rm -rf build" from "make && rm -rf build; ls".
func Segment(s string, i int) string {
	start, end := 0, len(s)
	for _, loc := range segmentSep.FindAllStringIndex(s, -1) {
		switch {
		case loc[1] <= i:
			start = loc[1]
		case loc[0] >= i && end == len(s):
			end = loc[0]
		}
	}
	return strings.TrimSpace(s[start:end])
}

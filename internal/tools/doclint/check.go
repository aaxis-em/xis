package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Warning is one problem in a document.
type Warning struct {
	Line int
	Msg  string
}

const (
	maxProcedural  = 20
	maxDescriptive = 25
)

// nonPreferred maps a non-preferred word to the preferred word.
var nonPreferred = map[string]string{
	"utilize": "use", "utilise": "use", "utilizes": "uses", "utilized": "used",
	"initiate": "start", "initiates": "starts", "initiated": "started",
	"facilitate": "help", "facilitates": "helps",
	"approximately": "about", "commence": "start", "terminate": "stop",
	"leverage": "use", "in order to": "to", "prior to": "before",
	"subsequently": "then", "numerous": "many", "additional": "more",
	"sufficient": "enough", "obtain": "get", "obtains": "gets",
	"perform": "do", "performs": "does", "via": "through",
	"simply": "(remove)", "just": "(remove)", "easily": "(remove)",
	"display": "show", "displays": "shows", "displayed": "shown",
	"warning flag": "flag", "alert": "flag", "alerts": "flags",
}

// imperative verbs that start procedural sentences.
var imperative = map[string]bool{
	"run": true, "add": true, "use": true, "set": true, "open": true, "install": true,
	"create": true, "delete": true, "remove": true, "start": true, "stop": true,
	"type": true, "press": true, "write": true, "read": true, "check": true,
	"make": true, "do": true, "give": true, "put": true, "copy": true, "edit": true,
	"go": true, "push": true, "commit": true, "select": true, "enter": true,
	"see": true, "keep": true, "change": true, "build": true, "clone": true,
}

var (
	reInlineCode = regexp.MustCompile("`[^`]*`")
	reLink       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	rePassive    = regexp.MustCompile(`(?i)\b(is|are|was|were|been|be|being)\s+(\w+ed)\b`)
	reListMarker = regexp.MustCompile(`^\s*([-*+]|\d+\.)\s+`)
	reNumbered   = regexp.MustCompile(`^\s*\d+\.\s+`)
	reWord       = regexp.MustCompile(`[A-Za-z0-9_'’-]+`)
)

// passiveExceptions holds "-ed" words that are adjectives in normal use.
var passiveExceptions = map[string]bool{
	"need": true, "based": true, "installed": true, "enabled": true,
	"disabled": true, "required": true, "supported": true, "signed": true,
	"suppressed": true, "unattributed": true, "attributed": true, "verified": true,
	"unverified": true, "imported": true, "redacted": true, "configured": true,
	"allowed": true, "changed": true, "deleted": true,
	"limited": true, "shared": true, "tracked": true, "untracked": true,
	"reviewed": true, "unreviewed": true, "understood": true, "questioned": true,
	"related": true, "interested": true, "expected": true, "fixed": true,
}

// Check returns warnings for the Markdown text. It skips code blocks,
// tables, headings, HTML comments and quoted agent text.
func Check(text string) []Warning {
	var out []Warning
	inFence := false
	inComment := false
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~"):
			inFence = !inFence
			continue
		case inFence:
			continue
		case strings.HasPrefix(trim, "<!--"):
			inComment = !strings.Contains(trim, "-->")
			continue
		case inComment:
			if strings.Contains(trim, "-->") {
				inComment = false
			}
			continue
		case trim == "", strings.HasPrefix(trim, "#"), strings.HasPrefix(trim, "|"):
			continue
		case strings.HasPrefix(trim, "> \"") || strings.HasPrefix(trim, ">\""):
			continue // quoted agent text
		}
		procedural := reNumbered.MatchString(line)
		body := reListMarker.ReplaceAllString(trim, "")
		body = strings.TrimPrefix(body, "> ")
		body = reLink.ReplaceAllString(body, "$1")
		body = reInlineCode.ReplaceAllString(body, "CODE")
		body = strings.NewReplacer("**", "", "__", "", "_", " ").Replace(body)
		for _, s := range splitSentences(body) {
			words := reWord.FindAllString(s, -1)
			if len(words) == 0 {
				continue
			}
			limit := maxDescriptive
			kind := "descriptive"
			if procedural || imperative[strings.ToLower(words[0])] {
				limit, kind = maxProcedural, "procedural"
			}
			if len(words) > limit {
				out = append(out, Warning{i + 1, fmt.Sprintf("%s sentence has %d words (limit %d): %q", kind, len(words), limit, short(s))})
			}
			for _, m := range rePassive.FindAllStringSubmatch(s, -1) {
				if passiveExceptions[strings.ToLower(m[2])] {
					continue
				}
				out = append(out, Warning{i + 1, fmt.Sprintf("possible passive voice %q", m[0])})
			}
			lower := " " + strings.ToLower(strings.Join(words, " ")) + " "
			for bad, good := range nonPreferred {
				if strings.Contains(lower, " "+bad+" ") {
					out = append(out, Warning{i + 1, fmt.Sprintf("non-preferred word %q: use %q", bad, good)})
				}
			}
		}
	}
	return out
}

// splitSentences splits text at ". ", "! " and "? ". It keeps "e.g." and
// file names such as "config.toml" in one sentence because the split needs
// a space after the period.
func splitSentences(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c == '.' || c == '!' || c == '?') && (i+1 == len(s) || s[i+1] == ' ') {
			if c == '.' && isAbbrev(s[start:i+1]) {
				continue
			}
			out = append(out, strings.TrimSpace(s[start:i+1]))
			start = i + 1
		}
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

func isAbbrev(s string) bool {
	for _, a := range []string{"e.g.", "i.e.", "etc.", "vs.", "approx."} {
		if strings.HasSuffix(strings.ToLower(s), a) {
			return true
		}
	}
	return false
}

func short(s string) string {
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

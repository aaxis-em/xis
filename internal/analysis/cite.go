package analysis

import (
	"regexp"
	"strings"
)

const NoEvidence = "No evidence-backed explanation."
const NotInRecord = "Not in the session record."

var citationRe = regexp.MustCompile(`\[(step|hunk):([A-Za-z0-9]+)\]`)

type Citations struct {
	Steps []string
	Hunks []string
}

// Validate keeps only sentences with at least one valid citation, removes
// invalid citations, and maps alias IDs to real IDs. The map functions return
// "" for an unknown alias. A sentence that says the answer is not in the
// record is kept without a citation when keepNotInRecord is true.
func Validate(text string, stepID, hunkID func(string) string, keepNotInRecord bool) (string, Citations) {
	var cites Citations
	seen := map[string]bool{}
	var outLines []string
	for _, line := range strings.Split(text, "\n") {
		prefix, body := splitPrefix(line)
		var kept []string
		for _, sentence := range splitSentences(body) {
			valid := 0
			fixed := citationRe.ReplaceAllStringFunc(sentence, func(m string) string {
				p := citationRe.FindStringSubmatch(m)
				real := ""
				if p[1] == "step" {
					real = stepID(p[2])
				} else {
					real = hunkID(p[2])
				}
				if real == "" {
					return ""
				}
				valid++
				key := p[1] + ":" + real
				if !seen[key] {
					seen[key] = true
					if p[1] == "step" {
						cites.Steps = append(cites.Steps, real)
					} else {
						cites.Hunks = append(cites.Hunks, real)
					}
				}
				return "[" + p[1] + ":" + real + "]"
			})
			if valid > 0 || (keepNotInRecord && strings.Contains(sentence, NotInRecord)) {
				kept = append(kept, strings.TrimSpace(strings.Join(strings.Fields(fixed), " ")))
			}
		}
		if len(kept) > 0 {
			outLines = append(outLines, prefix+strings.Join(kept, " "))
		}
	}
	out := strings.TrimSpace(strings.Join(outLines, "\n"))
	if out == "" {
		return NoEvidence, Citations{}
	}
	return out, cites
}

var listPrefix = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+`)

func splitPrefix(line string) (string, string) {
	if m := listPrefix.FindString(line); m != "" {
		return m, line[len(m):]
	}
	return "", line
}

// splitSentences splits after ".", "!" or "?" when a space follows. A
// citation that follows the period stays with its sentence.
func splitSentences(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '.' && c != '!' && c != '?' {
			continue
		}
		j := i + 1
		for j < len(s) && s[j] == ' ' {
			k := j
			for k < len(s) && s[k] == ' ' {
				k++
			}
			if k < len(s) && s[k] == '[' && citationRe.MatchString(s[k:min(len(s), k+40)]) {
				loc := citationRe.FindStringIndex(s[k:])
				if loc != nil && loc[0] == 0 {
					j = k + loc[1]
					continue
				}
			}
			break
		}
		if j >= len(s) || s[j] == ' ' {
			out = append(out, s[start:j])
			start = j
			i = j - 1
		}
	}
	if strings.TrimSpace(s[start:]) != "" {
		out = append(out, s[start:])
	}
	return out
}

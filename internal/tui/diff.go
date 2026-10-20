package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/aaxis-em/xis/internal/model"
)

type diffLine struct {
	kind byte
	text string
	old  int
	new  int
}

func parseHunk(h model.Hunk) (string, []diffLine) {
	lines := strings.Split(strings.TrimSuffix(h.Patch, "\n"), "\n")
	if len(lines) == 0 {
		return "", nil
	}
	header := lines[0]
	if !strings.HasPrefix(header, "@@") {
		return "", []diffLine{{kind: ' ', text: h.Patch}}
	}
	oldN, newN := h.OldStart, h.NewStart
	var out []diffLine
	for _, l := range lines[1:] {
		k := byte(' ')
		if l != "" {
			k = l[0]
			l = l[1:]
		}
		switch k {
		case '+':
			out = append(out, diffLine{kind: '+', text: l, new: newN})
			newN++
		case '-':
			out = append(out, diffLine{kind: '-', text: l, old: oldN})
			oldN++
		case '\\':
		default:
			out = append(out, diffLine{kind: ' ', text: l, old: oldN, new: newN})
			oldN++
			newN++
		}
	}
	return header, out
}

type highlighter struct {
	color bool
	cache map[string][]string
}

func newHighlighter(color bool) *highlighter {
	return &highlighter{color: color, cache: map[string][]string{}}
}

// render returns one styled string per diff line. Results are cached per
// hunk, so only hunks that are on screen are ever highlighted.
func (hl *highlighter) render(h model.Hunk, st styles_) []string {
	if out, ok := hl.cache[h.ID]; ok {
		return out
	}
	_, lines := parseHunk(h)
	code := make([]string, len(lines))
	for i, l := range lines {
		code[i] = strings.ReplaceAll(l.text, "\t", "    ")
	}
	if hl.color {
		code = highlightLines(h.FilePath, code)
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		switch l.kind {
		case '+':
			out[i] = st.add.Render("+") + code[i]
		case '-':
			out[i] = st.del.Render("-") + code[i]
		default:
			out[i] = " " + code[i]
		}
	}
	hl.cache[h.ID] = out
	return out
}

func highlightLines(file string, lines []string) []string {
	lexer := lexers.Match(file)
	if lexer == nil {
		return lines
	}
	lexer = chroma.Coalesce(lexer)
	style := styles.Get("monokai")
	f := formatters.Get("terminal256")
	out := make([]string, len(lines))
	for i, l := range lines {
		it, err := lexer.Tokenise(nil, l)
		if err != nil {
			out[i] = l
			continue
		}
		var b strings.Builder
		if err := f.Format(&b, style, it); err != nil {
			out[i] = l
			continue
		}
		out[i] = strings.TrimRight(b.String(), "\n")
	}
	return out
}

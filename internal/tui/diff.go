package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

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
		// A rename, mode or binary change has no line numbers.
		out := make([]diffLine, len(lines))
		for i, l := range lines {
			out[i] = diffLine{kind: ' ', text: l}
		}
		return "", out
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

// hunkRange describes where the hunk is in the new file, with the function
// name that git puts after the second @@.
func hunkRange(h model.Hunk, header string) string {
	var s string
	switch {
	case header == "":
		s = "file change"
	case h.NewLines == 0:
		s = fmt.Sprintf("removed after line %d", h.NewStart)
	case h.NewLines == 1:
		s = fmt.Sprintf("line %d", h.NewStart)
	default:
		s = fmt.Sprintf("lines %d–%d", h.NewStart, h.NewStart+h.NewLines-1)
	}
	if i := strings.Index(header[min(2, len(header)):], "@@"); i >= 0 {
		if ctx := strings.TrimSpace(header[i+4:]); ctx != "" {
			s += " · " + ctx
		}
	}
	return s
}

type palette struct{ add, del, addEmph, delEmph string }

var (
	darkDiff  = palette{add: "#173323", del: "#3d1a1f", addEmph: "#2c6440", delEmph: "#7a2d38"}
	lightDiff = palette{add: "#e6ffec", del: "#ffebe9", addEmph: "#abf2bc", delEmph: "#ffc1c0"}
)

type highlighter struct {
	color  bool
	style  string
	bg     palette
	cache  map[string][]string
	lexers map[string]chroma.Lexer
}

func newHighlighter(color bool) *highlighter {
	return &highlighter{color: color, style: "monokai", bg: darkDiff, cache: map[string][]string{}, lexers: map[string]chroma.Lexer{}}
}

// lexer caches the lexer of each file name; finding one tries every lexer.
func (hl *highlighter) lexer(file string) chroma.Lexer {
	if l, ok := hl.lexers[file]; ok {
		return l
	}
	l := lexers.Match(file)
	if l != nil {
		l = chroma.Coalesce(l)
	}
	hl.lexers[file] = l
	return l
}

func gutterDigits(lines []diffLine) int {
	n := 1
	for _, l := range lines {
		n = max(n, len(strconv.Itoa(l.old)), len(strconv.Itoa(l.new)))
	}
	return n
}

func gutter(l diffLine, digits int) string {
	num := func(n int) string {
		if n == 0 {
			return strings.Repeat(" ", digits)
		}
		return fmt.Sprintf("%*d", digits, n)
	}
	return num(l.old) + " " + num(l.new) + " "
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// render returns one line of exactly width w per diff line: the old and new
// line numbers, the +/- mark, and the code. Results are cached per hunk and
// width, so only hunks that are on screen are ever highlighted.
func (hl *highlighter) render(h model.Hunk, st styles_, w int) []string {
	key := h.ID + "\x00" + strconv.Itoa(w)
	if out, ok := hl.cache[key]; ok {
		return out
	}
	_, lines := parseHunk(h)
	digits := gutterDigits(lines)
	out := make([]string, len(lines))
	if !hl.color {
		for i, l := range lines {
			out[i] = fit(gutter(l, digits)+string(l.kind)+" "+expandTabs(l.text), w)
		}
		hl.cache[key] = out
		return out
	}
	toks := hl.tokens(h.FilePath, lines)
	emph := wordEmphasis(lines)
	profile := lipgloss.ColorProfile()
	style := styles.Get(hl.style)
	for i, l := range lines {
		bg, emphBg, mark := "", "", "  "
		switch l.kind {
		case '+':
			bg, emphBg, mark = hl.bg.add, hl.bg.addEmph, "+ "
		case '-':
			bg, emphBg, mark = hl.bg.del, hl.bg.delEmph, "- "
		}
		var b strings.Builder
		b.WriteString(st.dim.Render(gutter(l, digits)))
		if l.kind == ' ' {
			b.WriteString(mark)
		} else {
			fg := "2"
			if l.kind == '-' {
				fg = "1"
			}
			b.WriteString(profile.String(mark).Foreground(profile.Color(fg)).Background(profile.Color(bg)).String())
		}
		b.WriteString(paint(toks[i], emph[i], style, profile, bg, emphBg))
		s := b.String()
		if lipgloss.Width(s) > w {
			s = ansi.Truncate(s, w, "…")
		} else if pad := w - lipgloss.Width(s); pad > 0 {
			s += profile.String(strings.Repeat(" ", pad)).Background(profile.Color(bg)).String()
		}
		out[i] = s
	}
	hl.cache[key] = out
	return out
}

// tokens highlights the old side (context and removed lines) and the new
// side (context and added lines) of the hunk as two texts, so a comment or
// string over many lines keeps its color. It returns the tokens of each line.
func (hl *highlighter) tokens(file string, lines []diffLine) [][]chroma.Token {
	lexer := hl.lexer(file)
	out := make([][]chroma.Token, len(lines))
	for i, l := range lines {
		out[i] = []chroma.Token{{Type: chroma.Text, Value: expandTabs(l.text)}}
	}
	if lexer == nil {
		return out
	}
	for _, side := range []byte{'-', '+'} {
		var idx []int
		var text strings.Builder
		for i, l := range lines {
			if l.kind == ' ' && side == '-' {
				continue
			}
			if l.kind == ' ' || l.kind == side {
				idx = append(idx, i)
				text.WriteString(expandTabs(l.text) + "\n")
			}
		}
		if len(idx) == 0 {
			continue
		}
		it, err := lexer.Tokenise(nil, text.String())
		if err != nil {
			continue
		}
		perLine := make([][]chroma.Token, len(idx))
		n := 0
		for _, t := range it.Tokens() {
			parts := strings.Split(t.Value, "\n")
			for j, p := range parts {
				if j > 0 {
					n++
				}
				if p != "" && n < len(perLine) {
					perLine[n] = append(perLine[n], chroma.Token{Type: t.Type, Value: p})
				}
			}
		}
		for j, i := range idx {
			out[i] = perLine[j]
		}
	}
	return out
}

// paint writes the tokens with the colors of the syntax style on the line
// background, and the bytes marked in emph on the stronger background.
func paint(toks []chroma.Token, emph []bool, style *chroma.Style, p termenv.Profile, bg, emphBg string) string {
	var b strings.Builder
	pos := 0
	for _, t := range toks {
		e := style.Get(t.Type)
		for len(t.Value) > 0 {
			n, strong := len(t.Value), false
			if emph != nil && pos < len(emph) {
				strong = emph[pos]
				n = 1
				for n < len(t.Value) && pos+n < len(emph) && emph[pos+n] == strong {
					n++
				}
			}
			s := p.String(t.Value[:n])
			if e.Colour.IsSet() {
				s = s.Foreground(p.Color(e.Colour.String()))
			}
			if e.Bold == chroma.Yes {
				s = s.Bold()
			}
			if strong {
				s = s.Background(p.Color(emphBg))
			} else if bg != "" {
				s = s.Background(p.Color(bg))
			}
			b.WriteString(s.String())
			t.Value = t.Value[n:]
			pos += n
		}
	}
	return b.String()
}

var wordRe = regexp.MustCompile(`[\pL\pN_]+|\s+|.`)

// wordEmphasis marks the changed words of each removed line that has an
// added line in the same place, and of that added line. A line that changed
// in most of its words gets no marks, as the marks would only add noise.
func wordEmphasis(lines []diffLine) [][]bool {
	out := make([][]bool, len(lines))
	for i := 0; i < len(lines); {
		if lines[i].kind != '-' {
			i++
			continue
		}
		dels := i
		for i < len(lines) && lines[i].kind == '-' {
			i++
		}
		adds := i
		for i < len(lines) && lines[i].kind == '+' {
			i++
		}
		for k := 0; k < min(adds-dels, i-adds); k++ {
			a, b := expandTabs(lines[dels+k].text), expandTabs(lines[adds+k].text)
			if ma, mb, ok := changedWords(a, b); ok {
				out[dels+k], out[adds+k] = ma, mb
			}
		}
	}
	return out
}

func changedWords(a, b string) ([]bool, []bool, bool) {
	const maxLen = 400
	if len(a) > maxLen || len(b) > maxLen || a == b {
		return nil, nil, false
	}
	ta, tb := wordRe.FindAllString(a, -1), wordRe.FindAllString(b, -1)
	// lcs[i][j] is the length of the longest common token run of ta[i:] and tb[j:].
	lcs := make([][]int, len(ta)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(tb)+1)
	}
	for i := len(ta) - 1; i >= 0; i-- {
		for j := len(tb) - 1; j >= 0; j-- {
			if ta[i] == tb[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	ma, mb := make([]bool, len(a)), make([]bool, len(b))
	i, j, pa, pb, changed := 0, 0, 0, 0, 0
	for i < len(ta) || j < len(tb) {
		switch {
		case i < len(ta) && j < len(tb) && ta[i] == tb[j]:
			pa, pb = pa+len(ta[i]), pb+len(tb[j])
			i, j = i+1, j+1
		case j < len(tb) && (i == len(ta) || lcs[i][j+1] >= lcs[i+1][j]):
			for k := pb; k < pb+len(tb[j]); k++ {
				mb[k] = true
			}
			changed += len(tb[j])
			pb += len(tb[j])
			j++
		default:
			for k := pa; k < pa+len(ta[i]); k++ {
				ma[k] = true
			}
			changed += len(ta[i])
			pa += len(ta[i])
			i++
		}
	}
	if changed*10 > (len(a)+len(b))*6 {
		return nil, nil, false
	}
	return ma, mb, true
}

package tui

import (
	"regexp"
	"strings"
)

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	bulletRe   = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	numberedRe = regexp.MustCompile(`^(\s*)(\d+[.)])\s+(.*)$`)
	boldRe     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	codeSpanRe = regexp.MustCompile("`([^`]+)`")
)

// renderMarkdown formats the Markdown of an LLM answer for the terminal:
// headings, lists, code blocks, **bold** and `code`. Answers are short, so
// this subset is enough. A Markdown library would bring text/template, and
// its reflection stops the linker from removing unused code in the binary.
func (m *Model) renderMarkdown(text string, width int) []string {
	width = max(width, 20)
	var out []string
	inCode := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			out = append(out, m.st.code.Render("  "+strings.ReplaceAll(line, "\t", "    ")))
			continue
		}
		if trim == "" {
			out = append(out, "")
			continue
		}
		if g := headingRe.FindStringSubmatch(trim); g != nil {
			out = append(out, wrap(m.st.title.Render(g[2]), width)...)
			continue
		}
		if g := bulletRe.FindStringSubmatch(line); g != nil {
			indent := strings.Repeat(" ", len(g[1]))
			out = append(out, hanging(m.inline(g[2]), width, indent+"• ", indent+"  ")...)
			continue
		}
		if g := numberedRe.FindStringSubmatch(line); g != nil {
			indent := strings.Repeat(" ", len(g[1]))
			out = append(out, hanging(m.inline(g[3]), width, indent+g[2]+" ", indent+strings.Repeat(" ", len(g[2])+1))...)
			continue
		}
		out = append(out, wrap(m.inline(trim), width)...)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func (m *Model) inline(s string) string {
	s = codeSpanRe.ReplaceAllStringFunc(s, func(c string) string { return m.st.code.Render(c[1 : len(c)-1]) })
	return boldRe.ReplaceAllStringFunc(s, func(b string) string { return m.st.title.Render(b[2 : len(b)-2]) })
}

// hanging wraps s after the prefix first and indents the next lines by rest.
func hanging(s string, width int, first, rest string) []string {
	lines := wrap(s, max(width-len([]rune(first)), 10))
	for i := range lines {
		if i == 0 {
			lines[i] = first + lines[i]
		} else {
			lines[i] = rest + lines[i]
		}
	}
	return lines
}

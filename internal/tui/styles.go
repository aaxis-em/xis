package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

type styles_ struct {
	header, brand, footer, title, dim, sel, focusTitle, add, del, high, medium, low, info, ok, warn, quote, code lipgloss.Style
}

// brandAccent is the orange of the xis mark in assets/logo.
const brandAccent = "#F5A524"

func newStyles(color bool) styles_ {
	if !color {
		lipgloss.SetColorProfile(termenv.Ascii)
		plain := lipgloss.NewStyle()
		return styles_{header: plain.Bold(true), brand: plain.Bold(true), footer: plain, title: plain.Bold(true), dim: plain, sel: plain.Reverse(true),
			focusTitle: plain.Bold(true).Underline(true), add: plain, del: plain, high: plain.Bold(true), medium: plain, low: plain,
			info: plain, ok: plain, warn: plain.Bold(true), quote: plain.Italic(true), code: plain}
	}
	return styles_{
		header:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("236")),
		footer:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		title:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")),
		dim:        lipgloss.NewStyle().Foreground(lipgloss.Color("242")),
		brand:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(brandAccent)).Background(lipgloss.Color("236")),
		code:       lipgloss.NewStyle().Foreground(lipgloss.Color("180")),
		sel:        lipgloss.NewStyle().Background(lipgloss.Color("238")).Foreground(lipgloss.Color("15")),
		focusTitle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		add:        lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		del:        lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		high:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9")),
		medium:     lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		low:        lipgloss.NewStyle().Foreground(lipgloss.Color("228")),
		info:       lipgloss.NewStyle().Foreground(lipgloss.Color("111")),
		ok:         lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		warn:       lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		quote:      lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("187")),
	}
}

func (s styles_) severity(sev string) lipgloss.Style {
	switch sev {
	case "high":
		return s.high
	case "medium":
		return s.medium
	case "low":
		return s.low
	}
	return s.info
}

func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(ansi.Wordwrap(para, w, " "), "\n")...)
	}
	var res []string
	for _, l := range out {
		for lipgloss.Width(l) > w {
			res = append(res, ansi.Truncate(l, w, ""))
			l = ansi.TruncateLeft(l, w, "")
		}
		res = append(res, l)
	}
	return res
}

func block(lines []string, w, h, scroll int) []string {
	if scroll > len(lines)-h {
		scroll = len(lines) - h
	}
	if scroll < 0 {
		scroll = 0
	}
	out := make([]string, h)
	for i := 0; i < h; i++ {
		if scroll+i < len(lines) {
			out[i] = fit(lines[scroll+i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return out
}

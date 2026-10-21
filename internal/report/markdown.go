package report

import (
	"fmt"
	"strings"
)

const (
	maxFlags    = 25
	maxUnits    = 12
	maxFiles    = 5
	maxTestRuns = 8
	maxClaims   = 8
)

func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func limit(n, max int, full bool) int {
	if full || n <= max {
		return n
	}
	return max
}

func intentGlyph(s string) string {
	switch s {
	case "addressed":
		return "✅"
	case "partial":
		return "◐"
	case "unaddressed":
		return "❌"
	}
	return "❔"
}

// Markdown keeps the report short (about 150 lines) unless full is true.
func Markdown(r Report, full bool) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	w("## xis provenance report")
	w("")
	var agents, models, fidelity []string
	steps := 0
	seen := map[string]bool{}
	for _, s := range r.Sessions {
		if !seen["a"+s.Agent] {
			seen["a"+s.Agent] = true
			agents = append(agents, strings.TrimSpace(s.Agent+" "+s.Version))
		}
		for _, m := range s.Models {
			if !seen["m"+m] {
				seen["m"+m] = true
				models = append(models, m)
			}
		}
		if !seen["f"+s.Fidelity] {
			seen["f"+s.Fidelity] = true
			fidelity = append(fidelity, s.Fidelity)
		}
		steps += s.Steps
	}
	if len(r.Sessions) == 0 {
		w("No agent session covers this change. All hunks are unattributed.")
	} else {
		w("**Agent:** %s · **Models:** %s · **Fidelity:** %s", strings.Join(agents, ", "), orNone(models), strings.Join(fidelity, ", "))
		durs := make([]string, len(r.Sessions))
		for i, s := range r.Sessions {
			durs[i] = s.Duration
		}
		w("**Sessions:** %d · **Steps:** %d · **Duration:** %s", len(r.Sessions), steps, strings.Join(durs, ", "))
	}
	w("**Change:** %s → %s · %s · +%d −%d · contributors: %s", r.Base, r.Target, plural(r.FilesChanged, "file", "files"),
		r.LinesAdded, r.LinesRemoved, contributors(r.Contributors))
	for _, s := range r.Sessions {
		if s.Fidelity == "imported" {
			w("")
			w("> Note: the session was imported from logs. The attribution is reconstructed and not exact.")
			break
		}
	}
	w("")

	w("### Intent")
	w("")
	if len(r.Intent) == 0 {
		w("%s", r.IntentNote)
	}
	for _, it := range r.Intent {
		w("- %s %s (%s)", intentGlyph(it.Status), it.Text, it.Status)
	}
	w("")

	w("### Flags")
	w("")
	if len(r.Flags) == 0 {
		w("No unsuppressed flags.")
	} else {
		w("| Severity | Rule | Location | Message |")
		w("| --- | --- | --- | --- |")
		n := limit(len(r.Flags), maxFlags, full)
		for _, f := range r.Flags[:n] {
			w("| %s | `%s` | `%s` | %s |", f.Severity, f.Rule, cell(f.Location), cell(clip(f.Message, 140)))
		}
		if n < len(r.Flags) {
			w("")
			w("…and %d more. Run `xis report --full` or `xis flags`.", len(r.Flags)-n)
		}
	}
	if len(r.Suppressed) > 0 {
		w("")
		w("Suppressed:")
		for _, f := range r.Suppressed[:limit(len(r.Suppressed), maxFlags, full)] {
			w("- `%s` at `%s`: %s", f.Rule, cell(f.Location), cell(f.Reason))
		}
	}
	w("")

	w("### Review units")
	w("")
	n := limit(len(r.Units), maxUnits, full)
	for i, u := range r.Units[:n] {
		status := ""
		switch u.Status {
		case "understood":
			status = " ✔"
		case "questioned":
			status = " ❓"
		}
		w("%d. **%s**%s", i+1, u.Title, status)
		for _, e := range u.Explanation {
			w("   %s", e)
		}
		files := u.Files
		more := ""
		if !full && len(files) > maxFiles {
			more = fmt.Sprintf(" and %d more", len(files)-maxFiles)
			files = files[:maxFiles]
		}
		quoted := make([]string, len(files))
		for j, f := range files {
			quoted[j] = "`" + f + "`"
		}
		w("   Files: %s%s", strings.Join(quoted, ", "), more)
	}
	if n < len(r.Units) {
		w("")
		w("…and %d more units.", len(r.Units)-n)
	}
	w("")

	w("### Verification")
	w("")
	if len(r.TestRuns) == 0 {
		w("No test run occurred in the session.")
	} else {
		start := 0
		if !full && len(r.TestRuns) > maxTestRuns {
			start = len(r.TestRuns) - maxTestRuns
			w("(last %d of %d test runs)", maxTestRuns, len(r.TestRuns))
		}
		for _, t := range r.TestRuns[start:] {
			w("- step %d `%s` → **%s**", t.Step, cell(clip(t.Command, 80)), t.Outcome)
		}
	}
	w("")
	w("Last test run: **%s**.", r.LastTestRun)
	var unverified []Claim
	for _, c := range r.Claims {
		if !c.Verified {
			unverified = append(unverified, c)
		}
	}
	if len(unverified) > 0 {
		w("")
		w("Unverified agent claims:")
		for _, c := range unverified[:limit(len(unverified), maxClaims, full)] {
			w("- `%s`: \"%s\"", c.File, cell(c.Quote))
		}
	}
	w("")
	w("---")
	ids := make([]string, len(r.Sessions))
	for i, s := range r.Sessions {
		ids[i] = short(s.ID)
	}
	w("<sub>xis %s · sessions %s · note `%s`</sub>", r.XisVersion, orNone(ids), r.NoteRef)
	return b.String()
}

func orNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}

func contributors(m map[string]int) string {
	var parts []string
	for _, k := range []string{"ai", "human", "mixed", "unknown"} {
		if m[k] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ") + " hunks"
}

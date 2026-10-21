package analysis

import (
	"path"
	"regexp"
	"strings"

	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

var (
	claimPassRe = regexp.MustCompile(`(?i)\b(tests?|specs?|suite|build)\b[^.]{0,40}\b(pass|passes|passed|passing|green|succeed\w*)\b|\ball tests\b`)
	claimFailRe = regexp.MustCompile(`(?i)\b(tests?|specs?|suite|build)\b[^.]{0,40}\b(fail|fails|failed|failing|broken|red)\b`)
	claimFileRe = regexp.MustCompile(`[A-Za-z0-9_./\-]+\.[A-Za-z][A-Za-z0-9]{0,5}\b`)
)

// Verify checks the checkable assertions of an agent claim against the
// record. v1 checks only claims about test results and about touched files.
// A claim with no checkable assertion is never verified.
func Verify(claim string, claimStep model.Step, d *store.SessionData) bool {
	if d == nil || strings.TrimSpace(claim) == "" {
		return false
	}
	checks, ok := 0, true
	if claimPassRe.MatchString(claim) && !claimFailRe.MatchString(claim) {
		checks++
		ok = ok && lastRunBefore(d, claimStep, model.OutcomePass)
	} else if claimFailRe.MatchString(claim) {
		checks++
		ok = ok && lastRunBefore(d, claimStep, model.OutcomeFail)
	}
	edited := map[string]bool{}
	for _, e := range d.Edits {
		if s, found := d.Step(e.StepID); found && s.Seq <= claimStep.Seq {
			edited[e.FilePath] = true
			edited[path.Base(e.FilePath)] = true
		}
	}
	for _, f := range claimFileRe.FindAllString(claim, -1) {
		if strings.Contains(f, "://") || !strings.Contains(f, ".") || strings.HasPrefix(f, ".") {
			continue
		}
		checks++
		ok = ok && (edited[f] || edited[path.Base(f)])
	}
	return checks > 0 && ok
}

func lastRunBefore(d *store.SessionData, claimStep model.Step, outcome string) bool {
	var last *model.TestRun
	for i := range d.TestRuns {
		if s, ok := d.Step(d.TestRuns[i].StepID); ok && s.Seq <= claimStep.Seq {
			last = &d.TestRuns[i]
		}
	}
	if last == nil {
		return false
	}
	if outcome == model.OutcomeFail {
		return last.Outcome == model.OutcomeFail || last.Outcome == model.OutcomeError
	}
	return last.Outcome == outcome
}

package rules

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/testdetect"
)

var zeroTime = time.Unix(0, 0)

var (
	assertionRe      = regexp.MustCompile(`\bassert\w*\b|\bexpect\(|\bt\.(Error|Errorf|Fatal|Fatalf|Fail|FailNow)\b|\brequire\.\w|\bshould\b|#\[should_panic|\bpytest\.raises\b|\bAssert\.\w`)
	skipRe           = regexp.MustCompile(`\bt\.Skip(f|Now)?\(|@pytest\.mark\.(skip|skipif|xfail)\b|\bpytest\.skip\(|@unittest\.skip|\.skip\(|\bxit\(|\bxdescribe\(|\bxtest\(|@Disabled\b|@Ignore\b|#\[ignore\]|^\s*(pending|skip)\b`)
	lintSuppressRe   = regexp.MustCompile(`//\s*nolint|#\s*noqa|eslint-disable|@ts-ignore|@ts-expect-error|@SuppressWarnings|#!?\[allow\(|#\s*type:\s*ignore|#\s*pylint:\s*disable|rubocop:disable`)
	sensitiveRe      = regexp.MustCompile(`(?i)(auth|login|password|passwd|token|crypto|session|permission|payment|billing|security|sanitiz)`)
	secretFileRe     = regexp.MustCompile(`(?i)(^|/)(\.env[^/]*|[^/]*\.pem|[^/]*\.key|id_rsa[^/]*|[^/]*credentials[^/]*|[^/]*\.p12)$`)
	secretInCmdRe    = regexp.MustCompile(`(?i)(^|[\s/'"=])(\.env(\.[\w.-]+)?|[\w.-]+\.pem|[\w.-]+\.key|id_rsa\w*|[\w.-]*credentials[\w.-]*|[\w.-]+\.p12)($|[\s'";|&>])`)
	destructiveRe    = regexp.MustCompile(`(?i)\brm\s+(-\w*r\w*f\w*|-\w*f\w*r\w*|--recursive\s+--force|--force\s+--recursive)\b|\bgit\s+reset\s+--hard\b|\bgit\s+push\s+.*(--force\b|-f\b)|\bgit\s+clean\s+-\w*f|\bgit\s+checkout\s+--\s+\.|\bdrop\s+table\b|\btruncate\s+(table\s+)?\w|\bchmod\s+-R\s+777\b`)
	historyRewriteRe = regexp.MustCompile(`\bgit\s+rebase\b|\bgit\s+commit\s+.*--amend\b|\bgit\s+filter-(branch|repo)\b|\bgit\s+push\s+.*(--force\b|--force-with-lease\b|-f\b)`)
	claimPassRe      = regexp.MustCompile(`(?i)\b(all\s+)?(the\s+)?(tests?|specs?|test suite|suite|build)\s+(now\s+|all\s+|still\s+)?(pass|passes|passed|passing|succeed|succeeds|succeeded|green|work|works)\b|\beverything\s+(works|passes)\b|\bworks\s+now\b`)
)

func init() {
	def("TEST_FILE_MODIFIED", model.SevMedium, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, f := range c.Files {
			if f.Deleted || !c.Tests.IsTestFile(f.Path) || !c.FileAgentTouched(f.Path) {
				continue
			}
			out = append(out, r.fileFlag(c, f.Path, fmt.Sprintf("The agent changed the test file %s.", f.Path)))
		}
		return out
	})

	def("TEST_FILE_DELETED", model.SevHigh, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, f := range c.Files {
			if f.Deleted && c.Tests.IsTestFile(f.Path) {
				out = append(out, r.fileFlag(c, f.Path, fmt.Sprintf("The test file %s was deleted.", f.Path)))
			}
		}
		return out
	})

	def("ASSERTIONS_REDUCED", model.SevHigh, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, f := range c.Files {
			if !c.Tests.IsTestFile(f.Path) {
				continue
			}
			added, removed := 0, 0
			for _, h := range c.HunksOf(f.Path) {
				for _, l := range c.Lines(h, '+') {
					added += len(assertionRe.FindAllString(l, -1))
				}
				for _, l := range c.Lines(h, '-') {
					removed += len(assertionRe.FindAllString(l, -1))
				}
			}
			if removed > added {
				out = append(out, r.fileFlag(c, f.Path, fmt.Sprintf("Assertions in %s went down from the base by %d (removed %d, added %d).", f.Path, removed-added, removed, added)))
			}
		}
		return out
	})

	def("TEST_SKIPPED", model.SevHigh, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, h := range c.Hunks {
			for _, l := range c.Lines(h, '+') {
				if m := skipRe.FindString(l); m != "" {
					out = append(out, r.hunkFlag(c, h, fmt.Sprintf("A test is skipped or disabled in %s (%q).", h.FilePath, strings.TrimSpace(m))))
					break
				}
			}
		}
		return out
	})

	def("SNAPSHOT_UPDATED", model.SevMedium, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, f := range c.Files {
			if testdetect.IsSnapshotFile(f.Path) {
				out = append(out, r.fileFlag(c, f.Path, fmt.Sprintf("The test snapshot %s changed.", f.Path)))
			}
		}
		for _, d := range c.Sessions {
			for _, tr := range d.TestRuns {
				if testdetect.UpdatesSnapshots(tr.Command) {
					out = append(out, r.flag(c, d.Session.ID, "step", tr.StepID, "", fmt.Sprintf("A test run updated snapshots: %s", tr.Command), tr.StepID))
				}
			}
		}
		return out
	})

	def("TEST_EDIT_AFTER_FAILURE", model.SevHigh, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, d := range c.Sessions {
			failing := ""
			for _, s := range d.Steps {
				if tr, ok := d.TestRunOf(s.ID); ok {
					switch {
					case testFailed(tr):
						failing = s.ID
					case tr.Outcome == model.OutcomePass:
						failing = ""
					}
					continue
				}
				if failing == "" || !c.agentStep(d, s) {
					continue
				}
				for _, e := range d.EditsOf(s.ID) {
					if c.Tests.IsTestFile(e.FilePath) {
						fail, _ := d.Step(failing)
						out = append(out, r.flag(c, d.Session.ID, "step", s.ID, "",
							fmt.Sprintf("The agent edited the test file %s after a failing test run (step %d) and before a passing run.", e.FilePath, fail.Seq), failing, s.ID))
						break
					}
				}
			}
		}
		return out
	})

	def("TESTS_NEVER_RUN", model.SevMedium, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, d := range c.Sessions {
			if len(d.TestRuns) > 0 || !sessionChangedSource(c, d.Session.ID) {
				continue
			}
			out = append(out, r.flag(c, d.Session.ID, "session", d.Session.ID, "", "The session changed source files, but no test run occurred."))
		}
		return out
	})

	def("NO_TEST_AFTER_LAST_EDIT", model.SevMedium, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, d := range c.Sessions {
			if len(d.TestRuns) == 0 {
				continue
			}
			lastTest := d.TestRuns[len(d.TestRuns)-1].StepID
			lt, _ := d.Step(lastTest)
			var lastEdit model.Step
			for _, s := range d.Steps {
				if !c.agentStep(d, s) {
					continue
				}
				for _, e := range d.EditsOf(s.ID) {
					if c.IsSourceFile(e.FilePath) || c.Tests.IsTestFile(e.FilePath) {
						lastEdit = s
					}
				}
			}
			if lastEdit.ID != "" && lastEdit.Seq > lt.Seq {
				out = append(out, r.flag(c, d.Session.ID, "step", lastEdit.ID, "",
					fmt.Sprintf("The last code edit (step %d) came after the last test run (step %d).", lastEdit.Seq, lt.Seq), lastTest, lastEdit.ID))
			}
		}
		return out
	})

	def("LAST_TEST_RUN_FAILED", model.SevHigh, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, d := range c.Sessions {
			if n := len(d.TestRuns); n > 0 && testFailed(d.TestRuns[n-1]) {
				tr := d.TestRuns[n-1]
				out = append(out, r.flag(c, d.Session.ID, "step", tr.StepID, "", fmt.Sprintf("The last test run of the session failed: %s (%s).", tr.Command, tr.Outcome), tr.StepID))
			}
		}
		return out
	})

	def("CLAIMED_PASS_UNVERIFIED", model.SevHigh, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, d := range c.Sessions {
			final := finalAgentMessage(d)
			if final.ID == "" {
				continue
			}
			m := claimPassRe.FindString(final.Text)
			if m == "" {
				continue
			}
			status := "no test run occurred"
			evidence := []string{final.ID}
			if n := len(d.TestRuns); n > 0 {
				last := d.TestRuns[n-1]
				if last.Outcome == model.OutcomePass {
					continue
				}
				status = "the last test run is " + last.Outcome
				evidence = append(evidence, last.StepID)
			}
			out = append(out, r.flag(c, d.Session.ID, "step", final.ID, "", fmt.Sprintf("The agent says %q, but %s.", m, status), evidence...))
		}
		return out
	})

	fileKind := func(id string, sev model.Severity, kind testdetect.ManifestKind, what string) {
		def(id, sev, func(r *rule, c *Context) []model.Flag {
			var out []model.Flag
			for _, f := range c.Files {
				if testdetect.Classify(f.Path) == kind {
					out = append(out, r.fileFlag(c, f.Path, fmt.Sprintf("The %s %s changed.", what, f.Path)))
				}
			}
			return out
		})
	}
	fileKind("DEPENDENCY_CHANGED", model.SevMedium, testdetect.Dependency, "dependency file")
	fileKind("CI_CONFIG_CHANGED", model.SevHigh, testdetect.CIConfig, "CI config")
	fileKind("BUILD_CONFIG_CHANGED", model.SevMedium, testdetect.BuildConfig, "build or lint config")

	def("LINT_SUPPRESSED", model.SevMedium, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, h := range c.Hunks {
			for _, l := range c.Lines(h, '+') {
				if m := lintSuppressRe.FindString(l); m != "" {
					out = append(out, r.hunkFlag(c, h, fmt.Sprintf("A lint suppression was added in %s (%q).", h.FilePath, strings.TrimSpace(m))))
					break
				}
			}
		}
		return out
	})

	def("OUT_OF_SCOPE", model.SevMedium, func(r *rule, c *Context) []model.Flag {
		sc := InferScope(c)
		if sc.Empty() {
			return nil
		}
		var out []model.Flag
		for _, f := range c.Files {
			if !c.FileAgentTouched(f.Path) || sc.Contains(f.Path) {
				continue
			}
			out = append(out, r.fileFlag(c, f.Path, fmt.Sprintf("%s is outside the inferred scope of the session (%s).", f.Path, sc.Describe())))
		}
		return out
	})

	def("SECRET_FILE_ACCESS", model.SevHigh, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, d := range c.Sessions {
			for _, s := range d.Steps {
				tc, ok := d.ToolCalls[s.ID]
				if !ok || !c.agentStep(d, s) {
					continue
				}
				var hit string
				for _, p := range claudecode.ToolPaths(tc.Input) {
					if secretFileRe.MatchString(c.RelPath(p)) {
						hit = c.RelPath(p)
					}
				}
				if hit == "" && (tc.Category == model.CatShell || tc.Category == model.CatTest) {
					if m := secretInCmdRe.FindStringSubmatch(claudecode.ToolCommand(tc.Input)); m != nil {
						hit = m[2]
					}
				}
				for _, e := range d.EditsOf(s.ID) {
					if hit == "" && secretFileRe.MatchString(e.FilePath) {
						hit = e.FilePath
					}
				}
				if hit != "" {
					out = append(out, r.flag(c, d.Session.ID, "step", s.ID, "", fmt.Sprintf("The agent accessed the secret file %s (%s).", hit, tc.ToolName), s.ID))
				}
			}
		}
		return out
	})

	def("SECRET_IN_CHANGE", model.SevHigh, func(r *rule, c *Context) []model.Flag {
		if c.Redactor == nil {
			return nil
		}
		var out []model.Flag
		for _, h := range c.Hunks {
			added := strings.Join(c.Lines(h, '+'), "\n")
			if fs := c.Redactor.Findings(added); len(fs) > 0 {
				out = append(out, r.hunkFlag(c, h, fmt.Sprintf("Added lines in %s contain a secret (rule %s).", h.FilePath, fs[0].Rule)))
			}
		}
		return out
	})

	def("SENSITIVE_PATH", model.SevMedium, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, f := range c.Files {
			m := sensitiveRe.FindString(f.Path)
			if m == "" {
				for _, h := range f.Hunks {
					if m = sensitiveRe.FindString(h.Section); m != "" {
						break
					}
				}
			}
			if m != "" {
				out = append(out, r.fileFlag(c, f.Path, fmt.Sprintf("%s touches a sensitive area (%q).", f.Path, strings.ToLower(m))))
			}
		}
		return out
	})

	cmdRule := func(id string, re *regexp.Regexp, what string) {
		def(id, model.SevHigh, func(r *rule, c *Context) []model.Flag {
			var out []model.Flag
			for _, d := range c.Sessions {
				for _, s := range d.Steps {
					cmd := c.command(d, s.ID)
					if cmd == "" || !c.agentStep(d, s) {
						continue
					}
					if m := re.FindString(cmd); m != "" {
						out = append(out, r.flag(c, d.Session.ID, "step", s.ID, "", fmt.Sprintf("The agent ran a %s: %s", what, oneLine(cmd)), s.ID))
					}
				}
			}
			return out
		})
	}
	cmdRule("DESTRUCTIVE_COMMAND", destructiveRe, "destructive command")
	cmdRule("GIT_HISTORY_REWRITE", historyRewriteRe, "command that rewrites git history")

	def("PERMISSION_CHANGED", model.SevMedium, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, f := range c.Files {
			if f.OldMode != "" && f.NewMode != "" && f.OldMode != f.NewMode && !f.Created && !f.Deleted {
				out = append(out, r.fileFlag(c, f.Path, fmt.Sprintf("The mode of %s changed from %s to %s.", f.Path, f.OldMode, f.NewMode)))
			}
		}
		return out
	})

	def("REVERT_REAPPLY", model.SevLow, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, d := range c.Sessions {
			type state struct{ added, removed, readded bool }
			files := map[string]map[string]*state{}
			flagged := map[string]bool{}
			for _, e := range d.Edits {
				lines := files[e.FilePath]
				if lines == nil {
					lines = map[string]*state{}
					files[e.FilePath] = lines
				}
				for _, l := range strings.Split(e.Patch, "\n") {
					if len(l) < 2 || strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") {
						continue
					}
					text := strings.TrimSpace(l[1:])
					if len(text) < 4 {
						continue
					}
					st := lines[text]
					if st == nil {
						st = &state{}
						lines[text] = st
					}
					switch l[0] {
					case '+':
						if st.removed {
							st.readded = true
						}
						st.added = true
					case '-':
						if st.added {
							st.removed = true
						}
					}
					if st.readded && !flagged[e.FilePath] {
						flagged[e.FilePath] = true
						out = append(out, r.flag(c, d.Session.ID, "file", e.FilePath, "",
							fmt.Sprintf("In %s, the agent added, removed and added again the same lines.", e.FilePath), e.StepID))
					}
				}
			}
		}
		return out
	})

	def("LONG_RETRY_LOOP", model.SevLow, func(r *rule, c *Context) []model.Flag {
		limit := c.Cfg.Rules.RetryLoopRuns
		if limit <= 0 {
			limit = 4
		}
		var out []model.Flag
		for _, d := range c.Sessions {
			run, best := 0, 0
			var ids, bestIDs []string
			for _, tr := range d.TestRuns {
				if testFailed(tr) {
					run++
					ids = append(ids, tr.StepID)
					if run > best {
						best, bestIDs = run, append([]string{}, ids...)
					}
				} else {
					run, ids = 0, nil
				}
			}
			if best >= limit {
				out = append(out, r.flag(c, d.Session.ID, "session", d.Session.ID, "", fmt.Sprintf("%d test runs failed in a row.", best), bestIDs...))
			}
		}
		return out
	})

	def("UNATTRIBUTED_CHANGE", model.SevMedium, func(r *rule, c *Context) []model.Flag {
		var out []model.Flag
		for _, h := range c.Hunks {
			prov := c.Provenance[h.ID]
			all := len(prov) > 0
			for _, p := range prov {
				if p.StepID != model.Unattributed {
					all = false
				}
			}
			if all || len(prov) == 0 {
				out = append(out, r.hunkFlag(c, h, fmt.Sprintf("No captured step wrote this hunk of %s.", h.FilePath)))
			}
		}
		return out
	})

	def("LARGE_CHANGE", model.SevInfo, func(r *rule, c *Context) []model.Flag {
		lines := 0
		for _, f := range c.Files {
			lines += f.Added + f.Removed
		}
		maxLines, maxFiles := c.Cfg.Rules.LargeChangeLines, c.Cfg.Rules.LargeChangeFiles
		if (maxLines > 0 && lines > maxLines) || (maxFiles > 0 && len(c.Files) > maxFiles) {
			session := ""
			if len(c.Change.SessionIDs) > 0 {
				session = c.Change.SessionIDs[0]
			}
			return []model.Flag{r.flag(c, session, "session", session, "large", fmt.Sprintf("The change is large: %d changed lines in %d files.", lines, len(c.Files)))}
		}
		return nil
	})
}

func sessionChangedSource(c *Context, sessionID string) bool {
	for _, h := range c.Hunks {
		if !c.IsSourceFile(h.FilePath) || h.ChangeType != model.ChangeBehavior {
			continue
		}
		for _, p := range c.Provenance[h.ID] {
			if c.StepSession[p.StepID] == sessionID && c.ActorKind[p.StepID] != "human" {
				return true
			}
		}
	}
	return false
}

func finalAgentMessage(d *store.SessionData) model.Step {
	for i := len(d.Steps) - 1; i >= 0; i-- {
		s := d.Steps[i]
		if s.Kind == model.StepAssistantMessage && d.Actors[s.ActorID].Kind == "agent" {
			return s
		}
	}
	return model.Step{}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:119] + "…"
	}
	return s
}

func stem(p string) string {
	b := path.Base(p)
	if i := strings.Index(b, "."); i > 0 {
		b = b[:i]
	}
	return strings.ToLower(b)
}

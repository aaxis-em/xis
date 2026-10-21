package rules

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/redact"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/testdetect"
)

type tb struct {
	t *testing.T
	c *Context
	d *store.SessionData
}

const agent, human = "actor-agent", "actor-human"

func newTB(t *testing.T) *tb {
	cfg := config.Default()
	red, err := redact.New(cfg.Redact)
	if err != nil {
		t.Fatal(err)
	}
	d := &store.SessionData{
		Session:   model.Session{ID: "s1", StartedAt: time.Unix(1000, 0)},
		Actors:    map[string]model.Actor{agent: {ID: agent, Kind: "agent"}, human: {ID: human, Kind: "human"}},
		ToolCalls: map[string]model.ToolCall{},
		StepIndex: map[string]int{},
	}
	c := &Context{
		Change:      model.Change{ID: "c1", SessionIDs: []string{"s1"}},
		Sessions:    []*store.SessionData{d},
		Provenance:  map[string][]model.HunkProvenance{},
		StepSession: map[string]string{},
		ActorKind:   map[string]string{},
		RawPatches:  map[string]string{},
		RepoTop:     "/repo",
		Cfg:         cfg,
		Tests:       testdetect.New(nil, nil),
		Redactor:    red,
	}
	return &tb{t: t, c: c, d: d}
}

func (b *tb) step(kind model.StepKind, actor, text string) string {
	id := fmt.Sprintf("st%02d", len(b.d.Steps)+1)
	b.d.StepIndex[id] = len(b.d.Steps)
	b.d.Steps = append(b.d.Steps, model.Step{ID: id, SessionID: "s1", ActorID: actor, Seq: len(b.d.Steps) + 1, Kind: kind, Text: text})
	b.c.StepSession[id] = "s1"
	b.c.ActorKind[id] = b.d.Actors[actor].Kind
	return id
}

func (b *tb) prompt(text string) string { return b.step(model.StepUserMessage, human, text) }
func (b *tb) say(text string) string    { return b.step(model.StepAssistantMessage, agent, text) }

func (b *tb) tool(name string, input map[string]any) string {
	id := b.step(model.StepToolCall, agent, name)
	in, _ := json.Marshal(input)
	cat := model.CatOther
	switch name {
	case "Read":
		cat = model.CatRead
	case "Grep":
		cat = model.CatSearch
	case "Edit", "Write":
		cat = model.CatEdit
	case "Bash":
		cat = model.CatShell
	}
	b.d.ToolCalls[id] = model.ToolCall{StepID: id, ToolName: name, Category: cat, Input: string(in), OK: true}
	return id
}

func (b *tb) test(cmd, outcome string) string {
	id := b.tool("Bash", map[string]any{"command": cmd})
	tc := b.d.ToolCalls[id]
	tc.Category = model.CatTest
	b.d.ToolCalls[id] = tc
	b.d.TestRuns = append(b.d.TestRuns, model.TestRun{StepID: id, Command: cmd, Outcome: outcome})
	return id
}

func (b *tb) edit(path, patch string) string {
	id := b.tool("Edit", map[string]any{"file_path": "/repo/" + path})
	b.d.Edits = append(b.d.Edits, model.Edit{ID: "e-" + id, StepID: id, FilePath: path, Patch: patch})
	return id
}

func (b *tb) humanEdit(path, patch string) string {
	id := b.step(model.StepHumanEdit, human, "human")
	b.d.Edits = append(b.d.Edits, model.Edit{ID: "e-" + id, StepID: id, FilePath: path, Patch: patch})
	return id
}

func (b *tb) hunk(path, body, stepID string) model.Hunk {
	h := model.Hunk{ID: fmt.Sprintf("h%02d", len(b.c.Hunks)+1), ChangeID: "c1", FilePath: path, NewStart: 1, Patch: "@@ -1,1 +1,1 @@\n" + body, ChangeType: model.ChangeBehavior}
	if b.c.Tests.IsTestFile(path) {
		h.ChangeType = model.ChangeTest
	}
	b.c.Hunks = append(b.c.Hunks, h)
	lines := strings.Count(body, "\n") + 1
	b.c.Provenance[h.ID] = []model.HunkProvenance{{HunkID: h.ID, StepID: stepID, Lines: lines}}
	fp, ok := b.c.FilePatch(path)
	if !ok {
		fp = gitx.FilePatch{Path: path}
	}
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "+") {
			fp.Added++
		} else if strings.HasPrefix(l, "-") {
			fp.Removed++
		}
	}
	b.setFile(fp)
	return h
}

func (b *tb) setFile(fp gitx.FilePatch) {
	for i := range b.c.Files {
		if b.c.Files[i].Path == fp.Path {
			b.c.Files[i] = fp
			return
		}
	}
	b.c.Files = append(b.c.Files, fp)
}

func (b *tb) run(id string) []model.Flag {
	for _, r := range All() {
		if r.ID() == id {
			return r.Check(b.c)
		}
	}
	b.t.Fatalf("no rule %s", id)
	return nil
}

func TestRules(t *testing.T) {
	cases := []struct {
		id       string
		positive func(b *tb)
		negative func(b *tb)
	}{
		{"TEST_FILE_MODIFIED",
			func(b *tb) { b.hunk("calc_test.go", "+\tt.Log(1)", b.edit("calc_test.go", "")) },
			func(b *tb) { b.hunk("calc_test.go", "+\tt.Log(1)", b.humanEdit("calc_test.go", "")) }},
		{"TEST_FILE_DELETED",
			func(b *tb) { b.setFile(gitx.FilePatch{Path: "tests/test_a.py", Deleted: true}) },
			func(b *tb) { b.setFile(gitx.FilePatch{Path: "a.py", Deleted: true}) }},
		{"ASSERTIONS_REDUCED",
			func(b *tb) {
				b.hunk("a_test.go", "-\tif x { t.Fatal(\"x\") }\n-\tassert.Equal(t, 1, y)\n+\t_ = y", b.edit("a_test.go", ""))
			},
			func(b *tb) {
				b.hunk("a_test.go", "-\tassert.Equal(t, 1, y)\n+\trequire.Equal(t, 1, y)", b.edit("a_test.go", ""))
			}},
		{"TEST_SKIPPED",
			func(b *tb) { b.hunk("a_test.go", "+\tt.Skip(\"flaky\")", b.edit("a_test.go", "")) },
			func(b *tb) { b.hunk("a_test.go", "+\tt.Log(\"skip nothing\")", b.edit("a_test.go", "")) }},
		{"SNAPSHOT_UPDATED",
			func(b *tb) { b.test("npx jest -u", "pass") },
			func(b *tb) { b.test("npx jest", "pass") }},
		{"TEST_EDIT_AFTER_FAILURE",
			func(b *tb) {
				b.test("go test ./...", "fail")
				b.edit("a_test.go", "+x")
				b.test("go test ./...", "pass")
			},
			func(b *tb) {
				b.test("go test ./...", "fail")
				b.edit("a.go", "+x")
				b.test("go test ./...", "pass")
				b.edit("a_test.go", "+y")
			}},
		{"TESTS_NEVER_RUN",
			func(b *tb) { b.hunk("a.go", "+x", b.edit("a.go", "+x")) },
			func(b *tb) { b.hunk("a.go", "+x", b.edit("a.go", "+x")); b.test("go test ./...", "pass") }},
		{"NO_TEST_AFTER_LAST_EDIT",
			func(b *tb) { b.test("go test ./...", "pass"); b.edit("a.go", "+x") },
			func(b *tb) { b.edit("a.go", "+x"); b.test("go test ./...", "pass"); b.edit("README.md", "+doc") }},
		{"LAST_TEST_RUN_FAILED",
			func(b *tb) { b.test("go test ./...", "pass"); b.test("go test ./...", "fail") },
			func(b *tb) { b.test("go test ./...", "fail"); b.test("go test ./...", "pass") }},
		{"CLAIMED_PASS_UNVERIFIED",
			func(b *tb) { b.test("go test ./...", "fail"); b.say("Done. All tests pass now.") },
			func(b *tb) { b.test("go test ./...", "pass"); b.say("Done. All tests pass now.") }},
		{"DEPENDENCY_CHANGED",
			func(b *tb) { b.hunk("go.mod", "+require x v1", b.edit("go.mod", "")) },
			func(b *tb) { b.hunk("mod.go", "+x", b.edit("mod.go", "")) }},
		{"CI_CONFIG_CHANGED",
			func(b *tb) { b.hunk(".github/workflows/ci.yml", "+x", b.edit(".github/workflows/ci.yml", "")) },
			func(b *tb) { b.hunk("docs/workflows.md", "+x", b.edit("docs/workflows.md", "")) }},
		{"BUILD_CONFIG_CHANGED",
			func(b *tb) { b.hunk("Dockerfile", "+RUN x", b.edit("Dockerfile", "")) },
			func(b *tb) { b.hunk("docker.go", "+x", b.edit("docker.go", "")) }},
		{"LINT_SUPPRESSED",
			func(b *tb) { b.hunk("a.go", "+\tx := 1 //nolint:errcheck", b.edit("a.go", "")) },
			func(b *tb) { b.hunk("a.go", "-\tx := 1 //nolint:errcheck\n+\tx := 1", b.edit("a.go", "")) }},
		{"OUT_OF_SCOPE",
			func(b *tb) {
				b.prompt("Fix the bug in src/auth/login.go")
				b.hunk("src/auth/login.go", "+x", b.edit("src/auth/login.go", ""))
				b.hunk("billing/invoice.go", "+y", b.edit("billing/invoice.go", ""))
			},
			func(b *tb) {
				b.prompt("Fix the bug in src/auth/login.go")
				b.tool("Read", map[string]any{"file_path": "/repo/billing/invoice.go"})
				b.hunk("src/auth/login.go", "+x", b.edit("src/auth/login.go", ""))
				b.hunk("billing/invoice.go", "+y", b.edit("billing/invoice.go", ""))
				b.hunk("src/auth/login_test.go", "+z", b.edit("src/auth/login_test.go", ""))
				b.hunk("go.mod", "+module x", b.edit("go.mod", ""))
			}},
		{"SECRET_FILE_ACCESS",
			func(b *tb) { b.tool("Bash", map[string]any{"command": "cat .env | head"}) },
			func(b *tb) { b.tool("Bash", map[string]any{"command": "cat environment.md"}) }},
		{"SECRET_IN_CHANGE",
			func(b *tb) {
				b.hunk("cfg.go", "+const key = \""+redact.FakeSecrets["github"]+"\"", b.edit("cfg.go", ""))
			},
			func(b *tb) { b.hunk("cfg.go", "+const key = os.Getenv(\"KEY\")", b.edit("cfg.go", "")) }},
		{"SENSITIVE_PATH",
			func(b *tb) { b.hunk("internal/auth/jwt.go", "+x", b.edit("internal/auth/jwt.go", "")) },
			func(b *tb) { b.hunk("internal/math/add.go", "+x", b.edit("internal/math/add.go", "")) }},
		{"DESTRUCTIVE_COMMAND",
			func(b *tb) { b.tool("Bash", map[string]any{"command": "rm -rf build && make"}) },
			func(b *tb) { b.tool("Bash", map[string]any{"command": "rm build/out.txt"}) }},
		{"GIT_HISTORY_REWRITE",
			func(b *tb) { b.tool("Bash", map[string]any{"command": "git commit --amend --no-edit"}) },
			func(b *tb) { b.tool("Bash", map[string]any{"command": "git commit -m fix"}) }},
		{"PERMISSION_CHANGED",
			func(b *tb) { b.setFile(gitx.FilePatch{Path: "run.sh", OldMode: "100644", NewMode: "100755"}) },
			func(b *tb) { b.setFile(gitx.FilePatch{Path: "run.sh", NewMode: "100755", Created: true}) }},
		{"REVERT_REAPPLY",
			func(b *tb) {
				b.edit("a.go", "+\treturn retry(x)")
				b.edit("a.go", "-\treturn retry(x)")
				b.edit("a.go", "+\treturn retry(x)")
			},
			func(b *tb) {
				b.edit("a.go", "+\treturn retry(x)")
				b.edit("a.go", "-\treturn retry(x)")
				b.edit("b.go", "+\treturn retry(x)\n+\tdone()")
				b.edit("b.go", "-\treturn retry(x)\n-\tdone()\n+\tlog()\n+\treturn retry(x)\n+\tdone()")
			}},
		{"LONG_RETRY_LOOP",
			func(b *tb) {
				for i := 0; i < 4; i++ {
					b.test("go test ./...", "fail")
				}
			},
			func(b *tb) {
				for i := 0; i < 3; i++ {
					b.test("go test ./...", "fail")
				}
				b.test("go test ./...", "pass")
				b.test("go test ./...", "fail")
			}},
		{"UNATTRIBUTED_CHANGE",
			func(b *tb) { b.hunk("a.go", "+x", model.Unattributed) },
			func(b *tb) { b.hunk("a.go", "+x", b.edit("a.go", "")) }},
		{"LARGE_CHANGE",
			func(b *tb) {
				for i := 0; i < 21; i++ {
					b.setFile(gitx.FilePatch{Path: fmt.Sprintf("f%d.go", i), Added: 1})
				}
			},
			func(b *tb) { b.setFile(gitx.FilePatch{Path: "a.go", Added: 500}) }},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		seen[tc.id] = true
		t.Run(tc.id+"/positive", func(t *testing.T) {
			b := newTB(t)
			tc.positive(b)
			flags := b.run(tc.id)
			if len(flags) == 0 {
				t.Fatal("rule did not fire")
			}
			for _, f := range flags {
				if f.RuleID != tc.id || f.Message == "" || f.Fingerprint == "" || f.ID == "" {
					t.Errorf("bad flag %+v", f)
				}
			}
		})
		t.Run(tc.id+"/negative", func(t *testing.T) {
			b := newTB(t)
			tc.negative(b)
			if flags := b.run(tc.id); len(flags) != 0 {
				t.Fatalf("rule fired: %+v", flags)
			}
		})
	}
	for _, r := range All() {
		if !seen[r.ID()] {
			t.Errorf("rule %s has no test case", r.ID())
		}
	}
	if len(All()) != 25 {
		t.Errorf("%d rules registered, want 25", len(All()))
	}
}

func TestRunConfig(t *testing.T) {
	b := newTB(t)
	b.hunk("go.mod", "+x", b.edit("go.mod", ""))
	b.c.Cfg.Rules.Disabled = []string{"tests_never_run"}
	b.c.Cfg.Rules.Severity = map[string]string{"DEPENDENCY_CHANGED": "high"}
	flags := Run(b.c)
	var dep bool
	for _, f := range flags {
		if f.RuleID == "TESTS_NEVER_RUN" {
			t.Error("disabled rule ran")
		}
		if f.RuleID == "DEPENDENCY_CHANGED" {
			dep = true
			if f.Severity != model.SevHigh {
				t.Errorf("severity override: %s", f.Severity)
			}
		}
	}
	if !dep {
		t.Error("DEPENDENCY_CHANGED missing")
	}
	again := Run(b.c)
	if len(again) != len(flags) || again[0].ID != flags[0].ID {
		t.Error("flag IDs are not stable")
	}
}

func TestMirrorStem(t *testing.T) {
	for in, want := range map[string]string{"calc_test.go": "calc", "tests/test_calc.py": "calc", "src/calc.spec.ts": "calc", "calc.test.js": "calc"} {
		if got := mirrorStem(in); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestOutOfScopeInference(t *testing.T) {
	flagged := func(b *tb) []string {
		var out []string
		for _, f := range b.run("OUT_OF_SCOPE") {
			out = append(out, f.TargetID)
		}
		return out
	}
	t.Run("shell reads before the first edit", func(t *testing.T) {
		b := newTB(t)
		b.prompt("Add hit counting to the shortener")
		b.tool("Bash", map[string]any{"command": "ls && cat store/store.go | head -40"})
		b.hunk("store/store.go", "+\thits int", b.edit("store/store.go", ""))
		b.hunk("billing/invoice.go", "+y", b.edit("billing/invoice.go", ""))
		if got := flagged(b); len(got) != 1 || got[0] != "billing/invoice.go" {
			t.Errorf("flagged %v", got)
		}
	})
	t.Run("prompt identifier in added lines", func(t *testing.T) {
		b := newTB(t)
		b.prompt("Return expires_at in the stats response")
		b.hunk("api/stats.go", "+\tExpiresAt *time.Time", b.edit("api/stats.go", ""))
		b.hunk("billing/invoice.go", "+y", b.edit("billing/invoice.go", ""))
		if got := flagged(b); len(got) != 1 || got[0] != "billing/invoice.go" {
			t.Errorf("flagged %v", got)
		}
	})
	t.Run("identifier-only scope that matches nothing", func(t *testing.T) {
		b := newTB(t)
		b.prompt("Make the retry_count configurable")
		b.hunk("a.go", "+x", b.edit("a.go", ""))
		b.hunk("b.go", "+y", b.edit("b.go", ""))
		if got := flagged(b); len(got) != 0 {
			t.Errorf("flagged %v", got)
		}
	})
	t.Run("a later prompt does not narrow an earlier turn", func(t *testing.T) {
		b := newTB(t)
		b.prompt("Make the short codes 8 characters long")
		b.hunk("store/store.go", "+\tconst codeLen = 8", b.edit("store/store.go", ""))
		b.hunk("api/handler_test.go", "+\twant := 8", b.edit("api/handler_test.go", ""))
		b.prompt("Skip the flaky test in store/store_test.go")
		b.hunk("store/store_test.go", "+\tt.Skip(\"flaky\")", b.edit("store/store_test.go", ""))
		b.hunk("billing/invoice.go", "+y", b.edit("billing/invoice.go", ""))
		if got := flagged(b); len(got) != 1 || got[0] != "billing/invoice.go" {
			t.Errorf("flagged %v", got)
		}
	})
	t.Run("named path that the agent never touched", func(t *testing.T) {
		b := newTB(t)
		b.prompt("Fix the bug in auth/login.go")
		b.hunk("billing/invoice.go", "+y", b.edit("billing/invoice.go", ""))
		if got := flagged(b); len(got) != 1 {
			t.Errorf("flagged %v", got)
		}
	})
}

func TestHunkFlagsPointToTheLine(t *testing.T) {
	body := " package a\n-\told()\n \tx := 1\n+\ty := 2\n+\t%s\n \treturn"
	for id, line := range map[string]string{
		"TEST_SKIPPED":     `t.Skip("flaky")`,
		"LINT_SUPPRESSED":  "z := 3 //nolint:errcheck",
		"SECRET_IN_CHANGE": `key := "` + redact.FakeSecrets["github"] + `"`,
	} {
		b := newTB(t)
		h := b.hunk("a_test.go", fmt.Sprintf(body, line), b.edit("a_test.go", ""))
		h.NewStart = 10
		b.c.Hunks[0] = h
		fs := b.run(id)
		if len(fs) != 1 || fs[0].Line != 13 {
			t.Errorf("%s: flags %+v, want one at line 13", id, fs)
		}
	}
}

func TestDependencyChangedNeedsDependencyLines(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		want       bool
	}{
		{"go.mod", "+module linkr\n+\n+go 1.26", false},
		{"go.mod", "-go 1.25\n+go 1.26", false},
		{"go.mod", "+module linkr\n+\n+require github.com/x/y v1.2.3", true},
		{"go.mod", "+\tgithub.com/x/y v1.2.3 // indirect", true},
		{"package.json", "+{\n+  \"name\": \"web\",\n+  \"version\": \"0.1.0\",\n+  \"private\": true\n+}", false},
		{"package.json", "+    \"react\": \"^18.3.1\",", true},
		{"Cargo.toml", "+[package]\n+name = \"x\"\n+version = \"0.1.0\"\n+edition = \"2021\"\n+\n+[dependencies]", false},
		{"Cargo.toml", "+serde = \"1\"", true},
		{"requirements.txt", "+requests==2.32.0", true},
		{"go.sum", "+\n", true},
	} {
		b := newTB(t)
		b.hunk(tc.path, tc.body, b.edit(tc.path, ""))
		if got := len(b.run("DEPENDENCY_CHANGED")) == 1; got != tc.want {
			t.Errorf("%s %q: flagged %v, want %v", tc.path, tc.body, got, tc.want)
		}
	}
}

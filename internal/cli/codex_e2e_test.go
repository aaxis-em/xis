package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/config"
)

const codexFixture = "../../testdata/fixtures/codex/rollout-2026-10-20T09-00-00-019a2b3c-4d5e-7f60-8a9b-0c1d2e3f4a5b.jsonl"

func TestE2ECodexImport(t *testing.T) {
	isolate(t)
	repo := filepath.Join(t.TempDir(), "calc")
	if out, err := exec.Command("sh", "../../testdata/repos/calc.sh", repo).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("calc.go", "package calc\n\n// Add returns the sum of a and b.\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	write("util.go", "package calc\n\nconst Version = \"1.1\"\n")

	out, err := run(t, "--repo", repo, "import", "codex", "--path", codexFixture, "--base", "HEAD")
	if err != nil || !strings.Contains(out, "fidelity: imported") {
		t.Fatalf("import: %v\n%s", err, out)
	}
	if again, err := run(t, "--repo", repo, "import", "codex", "--path", codexFixture, "--base", "HEAD"); err != nil || !strings.Contains(again, "imported") {
		t.Fatalf("second import: %v %s", err, again)
	}

	st := openTestStore(t)
	app := &App{Paths: config.DefaultPaths(), Cfg: config.Default(), RepoDir: repo}
	sessions, _ := st.ListSessions("", 0)
	if len(sessions) != 1 {
		t.Fatalf("sessions after two imports = %d", len(sessions))
	}
	_, res, err := app.computeChange(st, attribution.Options{Mode: attribution.ModeSession})
	if err != nil {
		t.Fatal(err)
	}
	got := provenanceByFile(t, st, res)
	if strings.Join(got["calc.go"], ",") != "call_patch1:4" {
		t.Errorf("calc.go provenance = %v", got["calc.go"])
	}
	if strings.Join(got["util.go"], ",") != "unattributed:2" {
		t.Errorf("util.go provenance = %v (a shell-only change must stay unattributed)", got["util.go"])
	}

	fl, err := run(t, "--repo", repo, "--json", "flags", "--fail-on", "high")
	var ec exitCode
	if err != nil && !errors.As(err, &ec) {
		t.Fatal(err)
	}
	var f struct {
		Flags []struct {
			RuleID   string `json:"RuleID"`
			Location string `json:"location"`
		} `json:"flags"`
	}
	_ = json.Unmarshal([]byte(fl), &f)
	unattributed := ""
	for _, x := range f.Flags {
		if x.RuleID == "UNATTRIBUTED_CHANGE" {
			unattributed += x.Location
		}
	}
	if !strings.Contains(unattributed, "util.go") || strings.Contains(unattributed, "calc.go") {
		t.Errorf("UNATTRIBUTED_CHANGE flags at %q:\n%s", unattributed, fl)
	}

	rep, err := run(t, "--repo", repo, "report")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"**Fidelity:** imported", "reconstructed and not exact", "codex 0.47.0", "gpt-5-codex", "go test ./..."} {
		if !strings.Contains(rep, w) {
			t.Errorf("report misses %q:\n%s", w, rep)
		}
	}
	ca, err := app.analyzeChange(st, attribution.Options{Mode: attribution.ModeSession})
	if err != nil {
		t.Fatal(err)
	}
	d, err := buildReviewData(st, ca, false)
	if err != nil || d.Fidelity != "imported" || d.Agent != "codex" {
		t.Errorf("review data fidelity=%q agent=%q err=%v", d.Fidelity, d.Agent, err)
	}
	why, _ := run(t, "--repo", repo, "why", filepath.Join(repo, "calc.go")+":6")
	if !strings.Contains(why, "apply_patch calc.go") || !strings.Contains(why, "(imported)") {
		t.Errorf("why:\n%s", why)
	}
}

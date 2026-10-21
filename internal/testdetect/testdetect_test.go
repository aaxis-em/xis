package testdetect

import (
	"testing"

	"github.com/aaxis-em/xis/internal/model"
)

func TestIsTestFile(t *testing.T) {
	d := New([]string{"checks/**"}, nil)
	yes := []string{"a/b_test.go", "test_x.py", "pkg/x_test.py", "tests/unit/a.py", "conftest.py", "src/a.test.ts", "web/b.spec.js",
		"src/__tests__/a.js", "src/test/java/A.java", "spec/a_spec.rb", "test/a.rb", "src/__snapshots__/a.snap", "x.snap", "checks/a.sh"}
	no := []string{"a.go", "testing.go", "src/contest.py", "latest/a.go", "spec.md", "attest/x.go"}
	for _, f := range yes {
		if !d.IsTestFile(f) {
			t.Errorf("want test file: %s", f)
		}
	}
	for _, f := range no {
		if d.IsTestFile(f) {
			t.Errorf("want non-test file: %s", f)
		}
	}
}

func TestFramework(t *testing.T) {
	d := New(nil, []string{`^\./run-checks`})
	cases := map[string]string{
		"go test ./...": "go", "cd x && go test -run X ./pkg": "go", "python -m pytest -q": "pytest", "pytest tests/": "pytest",
		"npm test": "npm", "pnpm run test": "npm", "yarn test": "npm", "bun test": "npm", "npx jest": "jest", "npx vitest run": "vitest",
		"cargo test": "cargo", "mvn -q test": "maven", "./gradlew test": "gradle", "bundle exec rspec": "rspec", "rspec": "rspec",
		"vendor/bin/phpunit": "phpunit", "dotnet test": "dotnet", "make test": "make", "./run-checks --all": "custom",
	}
	for cmd, want := range cases {
		got, ok := d.Framework(cmd)
		if !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", cmd, got, ok, want)
		}
	}
	for _, cmd := range []string{"go build ./...", "ls tests", "git commit -m 'add test'", "cat pytest.ini", "echo go test"} {
		if f, ok := d.Framework(cmd); ok {
			t.Errorf("%q: unexpected framework %q", cmd, f)
		}
	}
}

func TestDetectSegment(t *testing.T) {
	d := New(nil, []string{`^\./run-checks`})
	for cmd, want := range map[string]string{
		"go test ./...":                            "go test ./...",
		"go vet ./... && go test -race ./...":      "go test -race ./...",
		"python3 - <<'E'\nx = 1\nE\ngo test ./...": "go test ./...",
		"cd web && CI=1 npm test 2>&1 | tail -5":   "npm test 2>&1",
		"make lint; ./run-checks --all":            "./run-checks --all",
	} {
		if _, got, ok := d.Detect(cmd); !ok || got != want {
			t.Errorf("Detect(%q) = %q, %v; want %q", cmd, got, ok, want)
		}
	}
}

func ip(n int) *int { return &n }

func TestOutcome(t *testing.T) {
	cases := []struct {
		name, cmd, out string
		exit           *int
		want           string
	}{
		{"go pass", "go test ./...", "ok  \tcalc\t0.003s\n", ip(0), model.OutcomePass},
		{"go fail", "go test ./...", "--- FAIL: TestAdd (0.00s)\nFAIL\nFAIL\tcalc\t0.003s\n", ip(1), model.OutcomeFail},
		{"go setup", "go test ./x", "FAIL\t./x [setup failed]\n", ip(1), model.OutcomeError},
		{"go setup no exit", "go test ./x", "FAIL\t./x [setup failed]\n", nil, model.OutcomeError},
		{"exit nonzero no summary", "go test ./...", "", ip(2), model.OutcomeFail},
		{"exit zero masked", "go test ./... ; echo done", "done", ip(0), model.OutcomeUnknown},
		{"exit zero masked but summary fail", "go test ./... | tail -3", "FAIL\tcalc\t0.1s\n", ip(0), model.OutcomeFail},
		{"exit zero plain", "go test ./...", "", ip(0), model.OutcomePass},
		{"no exit no summary", "go test ./...", "", nil, model.OutcomeUnknown},
		{"pytest", "pytest", "===== 3 passed, 1 failed in 0.1s =====", nil, model.OutcomeFail},
		{"pytest pass", "pytest", "===== 5 passed, 2 skipped in 0.1s =====", ip(0), model.OutcomePass},
		{"jest", "npx jest", "Tests:       1 failed, 4 passed, 5 total", nil, model.OutcomeFail},
		{"vitest", "npx vitest run", "      Tests  4 passed (4)", nil, model.OutcomePass},
		{"cargo", "cargo test", "test result: FAILED. 3 passed; 1 failed; 0 ignored; 0 measured", nil, model.OutcomeFail},
		{"rspec", "rspec", "10 examples, 0 failures, 2 pending", nil, model.OutcomePass},
		{"maven", "mvn test", "[INFO] BUILD SUCCESS", nil, model.OutcomePass},
	}
	for _, c := range cases {
		got := Outcome(c.cmd, c.exit, c.out)
		if got.Outcome != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got.Outcome, c.want)
		}
	}
	s := Outcome("pytest", nil, "===== 3 passed, 1 failed, 2 skipped in 0.1s =====")
	if s.Passed != 3 || s.Failed != 1 || s.Skipped != 2 {
		t.Errorf("counts %+v", s)
	}
}

func TestExitCodeFromText(t *testing.T) {
	if n := ExitCodeFromText("Exit code 3\nboom"); n == nil || *n != 3 {
		t.Errorf("got %v", n)
	}
	if ExitCodeFromText("ok") != nil {
		t.Error("want nil")
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]ManifestKind{
		"go.mod": Dependency, "web/package-lock.json": Dependency, "requirements-dev.txt": Dependency, "Cargo.toml": Dependency,
		".github/workflows/ci.yml": CIConfig, ".gitlab-ci.yml": CIConfig, "Jenkinsfile": CIConfig,
		"Makefile": BuildConfig, "Dockerfile.dev": BuildConfig, "tsconfig.base.json": BuildConfig, ".eslintrc.js": BuildConfig,
		"main.go": NotManifest, "README.md": NotManifest,
	}
	for f, want := range cases {
		if got := Classify(f); got != want {
			t.Errorf("%s: got %v want %v", f, got, want)
		}
	}
	if !IsGenerated("a.pb.go", "") || !IsGenerated("x.go", "// Code generated by stringer. DO NOT EDIT.\n") || IsGenerated("x.go", "package x") {
		t.Error("IsGenerated")
	}
	if !UpdatesSnapshots("npx jest -u") || !UpdatesSnapshots("vitest --update-snapshots") || UpdatesSnapshots("go test -run U") {
		t.Error("UpdatesSnapshots")
	}
}

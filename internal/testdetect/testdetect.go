package testdetect

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/shellcmd"
)

var DefaultGlobs = []string{
	"*_test.go",
	"test_*.py", "*_test.py", "tests/**", "conftest.py",
	"*.test.*", "*.spec.*", "__tests__/**",
	"src/test/**",
	"spec/**", "test/**",
	"__snapshots__/**", "*.snap",
}

var SnapshotGlobs = []string{"__snapshots__/**", "*.snap"}

type Detector struct {
	globs    []string
	commands []*regexp.Regexp
}

var defaultCommands = []struct {
	framework string
	re        *regexp.Regexp
}{
	{"go", regexp.MustCompile(`^go\s+test\b`)},
	{"pytest", regexp.MustCompile(`^((\S*/)?python3?\s+-m\s+)?(\S*/)?pytest\b`)},
	{"npm", regexp.MustCompile(`^(npm|pnpm|yarn|bun)\s+(run\s+)?test\b`)},
	{"jest", regexp.MustCompile(`^(npx\s+)?(\S*/)?jest\b`)},
	{"vitest", regexp.MustCompile(`^(npx\s+)?(\S*/)?vitest\b`)},
	{"mocha", regexp.MustCompile(`^npx\s+mocha\b`)},
	{"cargo", regexp.MustCompile(`^cargo\s+test\b`)},
	{"maven", regexp.MustCompile(`^(\S*/)?mvnw?\s+(\S+\s+)*test\b`)},
	{"gradle", regexp.MustCompile(`^(\S*/)?gradlew?\s+(\S+\s+)*test\b`)},
	{"rspec", regexp.MustCompile(`^(bundle\s+exec\s+)?(\S*/)?rspec\b`)},
	{"phpunit", regexp.MustCompile(`^(\S*/)?phpunit\b`)},
	{"dotnet", regexp.MustCompile(`^dotnet\s+test\b`)},
	{"make", regexp.MustCompile(`^make\s+(\S+\s+)*test\b`)},
}

var (
	segmentSep = regexp.MustCompile(`&&|\|\||[;|\n(]`)
	envPrefix  = regexp.MustCompile(`^(\w+=\S*\s+|time\s+|sudo\s+|exec\s+|command\s+)+`)
)

func segments(command string) []string {
	var out []string
	for _, seg := range segmentSep.Split(command, -1) {
		seg = envPrefix.ReplaceAllString(strings.TrimSpace(seg), "")
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

func New(extraGlobs, extraCommands []string) *Detector {
	d := &Detector{globs: append(append([]string{}, DefaultGlobs...), extraGlobs...)}
	for _, c := range extraCommands {
		if re, err := regexp.Compile(c); err == nil {
			d.commands = append(d.commands, re)
		}
	}
	return d
}

func MatchGlob(pattern, file string) bool {
	file = strings.TrimPrefix(file, "./")
	if dir, ok := strings.CutSuffix(pattern, "/**"); ok {
		return file == dir || strings.HasPrefix(file, dir+"/") || strings.Contains(file, "/"+dir+"/")
	}
	if strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, file)
		return ok
	}
	ok, _ := path.Match(pattern, path.Base(file))
	return ok
}

func (d *Detector) IsTestFile(file string) bool {
	for _, g := range d.globs {
		if MatchGlob(g, file) {
			return true
		}
	}
	return false
}

func IsSnapshotFile(file string) bool {
	for _, g := range SnapshotGlobs {
		if MatchGlob(g, file) {
			return true
		}
	}
	return false
}

func (d *Detector) Framework(command string) (string, bool) {
	fw, _, ok := d.Detect(command)
	return fw, ok
}

// Detect also returns the part of a compound command that runs the tests,
// for example "go test ./..." from "go vet ./... && go test ./...".
func (d *Detector) Detect(command string) (framework, segment string, ok bool) {
	segs := segments(shellcmd.WithoutHeredocs(command))
	for _, seg := range segs {
		for _, c := range defaultCommands {
			if c.re.MatchString(seg) {
				return c.framework, seg, true
			}
		}
	}
	for _, re := range d.commands {
		for _, seg := range segs {
			if re.MatchString(seg) {
				return "custom", seg, true
			}
		}
		if re.MatchString(command) {
			return "custom", command, true
		}
	}
	return "", "", false
}

var updateSnapshotsRe = regexp.MustCompile(`(^|\s)(-u|--update-snapshots?|--updateSnapshot|--snapshot-update)\b`)

func UpdatesSnapshots(command string) bool { return updateSnapshotsRe.MatchString(command) }

// MasksExitCode reports whether the shell exit status may not reflect the
// test command, for example "go test ./... ; echo done".
var singlePipe = regexp.MustCompile(`[^|]\|[^|]`)

func MasksExitCode(command string) bool {
	return strings.Contains(command, ";") || strings.Contains(command, "||") || singlePipe.MatchString(command) ||
		strings.Contains(command, "\n") || strings.HasSuffix(strings.TrimSpace(command), "&")
}

type Summary struct {
	Outcome                 string
	Passed, Failed, Skipped int
	Found                   bool
}

var (
	goOK         = regexp.MustCompile(`(?m)^ok\s+\S+`)
	goFail       = regexp.MustCompile(`(?m)^(FAIL|--- FAIL)\b`)
	goSetup      = regexp.MustCompile(`\[(setup|build) failed\]`)
	goPassCase   = regexp.MustCompile(`(?m)^\s*--- PASS`)
	goFailCase   = regexp.MustCompile(`(?m)^\s*--- FAIL`)
	goSkipCase   = regexp.MustCompile(`(?m)^\s*--- SKIP`)
	pyCounts     = regexp.MustCompile(`(\d+) (passed|failed|skipped|errors?|xfailed|xpassed)`)
	pyLine       = regexp.MustCompile(`(?m)^=+ .*(passed|failed|error|no tests ran).* =+$`)
	jestTests    = regexp.MustCompile(`(?m)^Tests:\s+(.*)$`)
	vitestTests  = regexp.MustCompile(`(?m)^\s*Tests\s+(.*)$`)
	countWord    = regexp.MustCompile(`(\d+) (passed|failed|skipped|todo)`)
	cargoResult  = regexp.MustCompile(`test result: (ok|FAILED)\. (\d+) passed; (\d+) failed; (\d+) ignored`)
	mavenResult  = regexp.MustCompile(`BUILD (SUCCESS|FAILURE)`)
	rspecResult  = regexp.MustCompile(`(\d+) examples?, (\d+) failures?(, (\d+) pending)?`)
	exitCodeLine = regexp.MustCompile(`(?m)^Exit code (\d+)`)
)

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func ParseSummary(out string) Summary {
	var s Summary
	switch {
	case goOK.MatchString(out) || goFail.MatchString(out):
		s.Found = true
		s.Passed = len(goPassCase.FindAllString(out, -1))
		s.Failed = len(goFailCase.FindAllString(out, -1))
		s.Skipped = len(goSkipCase.FindAllString(out, -1))
		if s.Passed == 0 && s.Failed == 0 {
			s.Passed = len(goOK.FindAllString(out, -1))
		}
		switch {
		case goSetup.MatchString(out):
			s.Outcome = model.OutcomeError
		case goFail.MatchString(out):
			s.Outcome = model.OutcomeFail
			if s.Failed == 0 {
				s.Failed = 1
			}
		default:
			s.Outcome = model.OutcomePass
		}
	case pyLine.MatchString(out):
		s.Found = true
		line := pyLine.FindAllString(out, -1)
		last := line[len(line)-1]
		for _, m := range pyCounts.FindAllStringSubmatch(last, -1) {
			switch m[2] {
			case "passed", "xpassed":
				s.Passed += atoi(m[1])
			case "failed":
				s.Failed += atoi(m[1])
			case "skipped", "xfailed":
				s.Skipped += atoi(m[1])
			case "error", "errors":
				s.Failed += atoi(m[1])
				s.Outcome = model.OutcomeError
			}
		}
		if s.Outcome == "" {
			switch {
			case s.Failed > 0:
				s.Outcome = model.OutcomeFail
			case s.Passed > 0:
				s.Outcome = model.OutcomePass
			default:
				s.Outcome = model.OutcomeUnknown
			}
		}
	case cargoResult.MatchString(out):
		s.Found = true
		for _, m := range cargoResult.FindAllStringSubmatch(out, -1) {
			s.Passed += atoi(m[2])
			s.Failed += atoi(m[3])
			s.Skipped += atoi(m[4])
		}
		s.Outcome = model.OutcomePass
		if s.Failed > 0 || strings.Contains(out, "test result: FAILED") {
			s.Outcome = model.OutcomeFail
		}
	case jestTests.MatchString(out) || vitestTests.MatchString(out):
		m := jestTests.FindStringSubmatch(out)
		if m == nil {
			m = vitestTests.FindStringSubmatch(out)
		}
		for _, c := range countWord.FindAllStringSubmatch(m[1], -1) {
			s.Found = true
			switch c[2] {
			case "passed":
				s.Passed += atoi(c[1])
			case "failed":
				s.Failed += atoi(c[1])
			default:
				s.Skipped += atoi(c[1])
			}
		}
		if s.Found {
			s.Outcome = model.OutcomePass
			if s.Failed > 0 {
				s.Outcome = model.OutcomeFail
			}
		}
	case rspecResult.MatchString(out):
		m := rspecResult.FindStringSubmatch(out)
		s.Found = true
		total, failed := atoi(m[1]), atoi(m[2])
		s.Failed, s.Skipped = failed, atoi(m[4])
		s.Passed = total - failed - s.Skipped
		s.Outcome = model.OutcomePass
		if failed > 0 {
			s.Outcome = model.OutcomeFail
		}
	case mavenResult.MatchString(out):
		s.Found = true
		s.Outcome = model.OutcomePass
		if strings.Contains(out, "BUILD FAILURE") {
			s.Outcome = model.OutcomeFail
		}
	}
	return s
}

func ExitCodeFromText(s string) *int {
	m := exitCodeLine.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	n := atoi(m[1])
	return &n
}

// Outcome never guesses "pass": a zero exit code counts only when the shell
// command cannot hide the exit code of the test command.
func Outcome(command string, exitCode *int, output string) Summary {
	s := ParseSummary(output)
	if exitCode != nil && *exitCode != 0 {
		if s.Outcome != model.OutcomeError {
			s.Outcome = model.OutcomeFail
		}
		return s
	}
	if s.Found && s.Outcome != "" {
		return s
	}
	if exitCode != nil && *exitCode == 0 && !MasksExitCode(command) {
		s.Outcome = model.OutcomePass
		return s
	}
	s.Outcome = model.OutcomeUnknown
	return s
}

type ManifestKind int

const (
	NotManifest ManifestKind = iota
	Dependency
	CIConfig
	BuildConfig
)

var dependencyFiles = map[string]bool{
	"go.mod": true, "go.sum": true, "go.work": true, "go.work.sum": true,
	"package.json": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "bun.lockb": true, "bun.lock": true, "npm-shrinkwrap.json": true,
	"pyproject.toml": true, "poetry.lock": true, "Pipfile": true, "Pipfile.lock": true, "setup.py": true, "setup.cfg": true, "uv.lock": true,
	"Cargo.toml": true, "Cargo.lock": true,
	"Gemfile": true, "Gemfile.lock": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true, "settings.gradle": true, "settings.gradle.kts": true, "gradle.lockfile": true,
	"composer.json": true, "composer.lock": true, "mix.exs": true, "mix.lock": true,
	"Podfile": true, "Podfile.lock": true, "Package.swift": true, "Package.resolved": true,
	"packages.config": true, "Directory.Packages.props": true,
}

var lockfiles = map[string]bool{
	"go.sum": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "bun.lockb": true, "bun.lock": true,
	"poetry.lock": true, "Pipfile.lock": true, "uv.lock": true, "Cargo.lock": true, "Gemfile.lock": true, "composer.lock": true,
	"mix.lock": true, "Podfile.lock": true, "Package.resolved": true, "gradle.lockfile": true, "npm-shrinkwrap.json": true,
}

func IsLockfile(file string) bool { return lockfiles[path.Base(file)] }

func Classify(file string) ManifestKind {
	base := path.Base(file)
	switch {
	case dependencyFiles[base], strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt"), strings.HasSuffix(base, ".csproj"):
		return Dependency
	case strings.HasPrefix(file, ".github/workflows/"), base == ".gitlab-ci.yml", strings.HasPrefix(file, ".circleci/"),
		base == "Jenkinsfile", base == "azure-pipelines.yml", base == ".travis.yml", strings.HasPrefix(file, ".buildkite/"):
		return CIConfig
	case base == "Makefile", base == "makefile", base == "GNUmakefile", strings.HasPrefix(base, "Dockerfile"), strings.HasPrefix(base, "docker-compose"),
		strings.HasPrefix(base, "compose.y"), strings.HasPrefix(base, "tsconfig"), strings.HasPrefix(base, ".eslintrc"), strings.HasPrefix(base, "eslint.config"),
		strings.HasPrefix(base, ".prettierrc"), strings.HasPrefix(base, "prettier.config"), base == ".golangci.yml", base == ".golangci.yaml",
		base == ".editorconfig", base == "ruff.toml", base == ".flake8", base == "tox.ini", base == ".pylintrc", base == "mypy.ini",
		base == "rustfmt.toml", base == "clippy.toml", base == ".rubocop.yml", base == "biome.json", base == ".stylelintrc",
		strings.HasPrefix(base, "webpack.config"), strings.HasPrefix(base, "vite.config"), strings.HasPrefix(base, "babel.config"), base == ".babelrc",
		strings.HasPrefix(base, "jest.config"), strings.HasPrefix(base, "vitest.config"), base == "CMakeLists.txt", base == "BUILD", base == "BUILD.bazel",
		base == "WORKSPACE", base == ".goreleaser.yaml", base == ".goreleaser.yml", base == "Taskfile.yml", base == "justfile":
		return BuildConfig
	}
	return NotManifest
}

func IsDocs(file string) bool {
	ext := strings.ToLower(path.Ext(file))
	return ext == ".md" || ext == ".rst" || ext == ".adoc" || strings.HasPrefix(file, "docs/") || strings.Contains(file, "/docs/") ||
		strings.EqualFold(path.Base(file), "LICENSE") || strings.EqualFold(path.Base(file), "CHANGELOG")
}

var generatedRe = regexp.MustCompile(`(?i)(code generated .* do not edit|@generated|autogenerated file|this file was automatically generated)`)

func IsGenerated(file, content string) bool {
	base := path.Base(file)
	if strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, "_pb2.py") || strings.HasSuffix(base, ".pb.ts") ||
		strings.HasSuffix(base, ".gen.go") || strings.HasSuffix(base, "_generated.go") || strings.HasSuffix(base, ".min.js") || IsLockfile(file) {
		return true
	}
	return generatedRe.MatchString(content)
}

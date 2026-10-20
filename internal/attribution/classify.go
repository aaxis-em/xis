package attribution

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spaolacci/murmur3"

	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/testdetect"
)

var spaceRun = regexp.MustCompile(`\s+`)

func normalize(line string) string {
	return strings.TrimSpace(spaceRun.ReplaceAllString(line, " "))
}

// ContentHash is the Agent Trace range hash: 32-bit MurmurHash3 (seed 0) of
// the added lines, whitespace-normalized and joined with "\n".
func ContentHash(added []string) string {
	if len(added) == 0 {
		return ""
	}
	norm := make([]string, 0, len(added))
	for _, l := range added {
		norm = append(norm, normalize(l))
	}
	return fmt.Sprintf("murmur3:%08x", murmur3.Sum32([]byte(strings.Join(norm, "\n"))))
}

func onlyWhitespace(lines []gitx.Line) bool {
	var added, removed []string
	for _, l := range lines {
		switch l.Kind {
		case '+':
			added = append(added, normalize(l.Text))
		case '-':
			removed = append(removed, normalize(l.Text))
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return false
	}
	return strings.Join(filterEmpty(added), "\n") == strings.Join(filterEmpty(removed), "\n")
}

func filterEmpty(s []string) []string {
	var out []string
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func ClassifyHunk(tests *testdetect.Detector, fp gitx.FilePatch, lines []gitx.Line) model.ChangeType {
	path := fp.Path
	var added strings.Builder
	for _, l := range lines {
		if l.Kind == '+' || l.Kind == ' ' {
			added.WriteString(l.Text + "\n")
		}
	}
	switch {
	case tests.IsTestFile(path):
		return model.ChangeTest
	case testdetect.IsGenerated(path, added.String()):
		return model.ChangeGenerated
	}
	switch testdetect.Classify(path) {
	case testdetect.Dependency:
		return model.ChangeDependency
	case testdetect.CIConfig, testdetect.BuildConfig:
		return model.ChangeConfig
	}
	switch {
	case testdetect.IsDocs(path):
		return model.ChangeDocs
	case fp.OldPath != "" && fp.Added == 0 && fp.Removed == 0:
		return model.ChangeRefactor
	case len(lines) > 0 && onlyWhitespace(lines):
		return model.ChangeRefactor
	case isConfigFile(path):
		return model.ChangeConfig
	}
	return model.ChangeBehavior
}

func isConfigFile(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range []string{".yml", ".yaml", ".toml", ".ini", ".cfg", ".conf", ".env.example"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

package notes

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/model"
)

// Agent Trace v0.1.0 (agent-trace.dev).
type ATRecord struct {
	Version   string         `json:"version"`
	ID        string         `json:"id"`
	Timestamp string         `json:"timestamp"`
	VCS       *ATVCS         `json:"vcs,omitempty"`
	Tool      *ATTool        `json:"tool,omitempty"`
	Files     []ATFile       `json:"files"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ATVCS struct {
	Type     string `json:"type"`
	Revision string `json:"revision"`
}

type ATTool struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type ATFile struct {
	Path          string           `json:"path"`
	Conversations []ATConversation `json:"conversations"`
}

type ATConversation struct {
	URL         string         `json:"url,omitempty"`
	Contributor *ATContributor `json:"contributor,omitempty"`
	Ranges      []ATRange      `json:"ranges"`
}

type ATContributor struct {
	Type    string `json:"type"`
	ModelID string `json:"model_id,omitempty"`
}

type ATRange struct {
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	ContentHash string `json:"content_hash,omitempty"`
}

type TraceInput struct {
	Revision    string
	Target      string
	Hunks       []model.Hunk
	RawPatches  map[string]string
	LineSteps   map[string]map[int]string
	StepSession map[string]string
	ActorKind   map[string]string
	Sessions    []model.Session
	XisVersion  string
	Now         time.Time
}

func uuid4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ModelID follows the models.dev "provider/model-name" convention.
func ModelID(m string) string {
	switch {
	case m == "" || strings.Contains(m, "/"):
		return m
	case strings.HasPrefix(m, "claude"):
		return "anthropic/" + m
	case strings.HasPrefix(m, "gpt") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") || strings.HasPrefix(m, "o4") || strings.Contains(m, "codex"):
		return "openai/" + m
	case strings.HasPrefix(m, "gemini"):
		return "google/" + m
	}
	return m
}

type lineInfo struct {
	line int
	text string
	key  string
}

func AgentTrace(in TraceInput) ATRecord {
	rec := ATRecord{Version: "0.1", ID: uuid4(), Timestamp: in.Now.UTC().Format(time.RFC3339),
		Tool: &ATTool{Name: "xis", Version: in.XisVersion}, Files: []ATFile{}}
	if in.Revision != "" {
		rec.VCS = &ATVCS{Type: "git", Revision: in.Revision}
	}
	models := map[string]string{}
	var ids []string
	fidelity := map[string]string{}
	for _, s := range in.Sessions {
		ids = append(ids, s.ID)
		fidelity[s.ID] = string(s.Fidelity)
		if len(s.Models) > 0 {
			models[s.ID] = ModelID(s.Models[0])
		}
	}
	rec.Metadata = map[string]any{"dev.xis": map[string]any{"sessions": ids, "fidelity": fidelity, "target": in.Target}}

	byFile := map[string][]lineInfo{}
	for _, h := range in.Hunks {
		patch := in.RawPatches[h.ID]
		if patch == "" {
			patch = h.Patch
		}
		_, body, ok := strings.Cut(patch, "\n")
		if !ok || !strings.HasPrefix(patch, "@@") {
			continue
		}
		newLine := h.NewStart
		for _, raw := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
			if raw == "" || raw[0] == ' ' {
				newLine++
				continue
			}
			if raw[0] != '+' {
				continue
			}
			l := struct {
				NewLine int
				Text    string
			}{newLine, raw[1:]}
			newLine++
			step := in.LineSteps[h.FilePath][l.NewLine]
			key := "unknown"
			switch {
			case step == "" || step == model.Unattributed:
			case in.ActorKind[step] == "human":
				key = "human"
			default:
				key = "ai:" + in.StepSession[step]
			}
			byFile[h.FilePath] = append(byFile[h.FilePath], lineInfo{line: l.NewLine, text: l.Text, key: key})
		}
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		lines := byFile[f]
		sort.Slice(lines, func(i, j int) bool { return lines[i].line < lines[j].line })
		convs := map[string]*ATConversation{}
		var order []string
		for i := 0; i < len(lines); {
			j := i
			for j+1 < len(lines) && lines[j+1].key == lines[i].key && lines[j+1].line == lines[j].line+1 {
				j++
			}
			key := lines[i].key
			c, ok := convs[key]
			if !ok {
				c = &ATConversation{Contributor: contributorFor(key, models)}
				convs[key] = c
				order = append(order, key)
			}
			texts := make([]string, 0, j-i+1)
			for _, l := range lines[i : j+1] {
				texts = append(texts, l.text)
			}
			c.Ranges = append(c.Ranges, ATRange{StartLine: lines[i].line, EndLine: lines[j].line, ContentHash: attribution.ContentHash(texts)})
			i = j + 1
		}
		af := ATFile{Path: f}
		for _, k := range order {
			af.Conversations = append(af.Conversations, *convs[k])
		}
		rec.Files = append(rec.Files, af)
	}
	return rec
}

func contributorFor(key string, models map[string]string) *ATContributor {
	switch {
	case key == "human":
		return &ATContributor{Type: "human"}
	case strings.HasPrefix(key, "ai:"):
		return &ATContributor{Type: "ai", ModelID: models[strings.TrimPrefix(key, "ai:")]}
	}
	return &ATContributor{Type: "unknown"}
}

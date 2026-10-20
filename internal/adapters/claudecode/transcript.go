package claudecode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type Entry struct {
	Type        string          `json:"type"`
	Subtype     string          `json:"subtype"`
	UUID        string          `json:"uuid"`
	ParentUUID  string          `json:"parentUuid"`
	Timestamp   time.Time       `json:"timestamp"`
	IsSidechain bool            `json:"isSidechain"`
	IsMeta      bool            `json:"isMeta"`
	SessionID   string          `json:"sessionId"`
	Version     string          `json:"version"`
	Cwd         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	AgentID     string          `json:"agentId"`
	Message     *Message        `json:"message"`
	Origin      *Origin         `json:"origin"`
	AITitle     string          `json:"aiTitle"`
	Content     json.RawMessage `json:"content"`
	ToolResult  json.RawMessage `json:"toolUseResult"`
}

type Origin struct {
	Kind string `json:"kind"`
}

type Message struct {
	ID      string  `json:"id"`
	Role    string  `json:"role"`
	Model   string  `json:"model"`
	Content Content `json:"content"`
	Usage   *Usage  `json:"usage"`
}

type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type Block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// Content is a string or a list of blocks in the transcript.
type Content []Block

func (c *Content) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*c = Content{{Type: "text", Text: s}}
		return nil
	}
	var blocks []Block
	if err := json.Unmarshal(b, &blocks); err != nil {
		return err
	}
	*c = blocks
	return nil
}

func (c Content) IsPlainString() bool {
	return len(c) == 1 && c[0].Type == "text"
}

// ToolResultText flattens the content of a tool_result block, which is a
// string or a list of text and image blocks.
func ToolResultText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var blocks []Block
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "":
		default:
			parts = append(parts, "["+b.Type+"]")
		}
	}
	return strings.Join(parts, "\n")
}

func ReadTranscript(path string) ([]Entry, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var out []Entry
	var warnings []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	n := 0
	for sc.Scan() {
		n++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			warnings = append(warnings, fmt.Sprintf("transcript line %d: %v", n, err))
			continue
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		warnings = append(warnings, "transcript: "+err.Error())
	}
	return out, warnings, nil
}

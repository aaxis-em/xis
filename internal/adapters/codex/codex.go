package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/adapters"
	"github.com/aaxis-em/xis/internal/model"
)

const Agent = "codex"

type line struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMeta struct {
	ID         string `json:"id"`
	Timestamp  string `json:"timestamp"`
	Cwd        string `json:"cwd"`
	Originator string `json:"originator"`
	CLIVersion string `json:"cli_version"`
	Git        *struct {
		CommitHash    string `json:"commit_hash"`
		Branch        string `json:"branch"`
		RepositoryURL string `json:"repository_url"`
	} `json:"git"`
	Meta *sessionMeta `json:"meta"`
}

type turnContext struct {
	Cwd   string `json:"cwd"`
	Model string `json:"model"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Summary   []contentItem   `json:"summary"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     string          `json:"input"`
	CallID    string          `json:"call_id"`
	Output    json.RawMessage `json:"output"`
	Action    *struct {
		Type    string   `json:"type"`
		Command []string `json:"command"`
	} `json:"action"`
}

type eventMsg struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Info    *struct {
		Total *struct {
			Input  int64 `json:"input_tokens"`
			Cached int64 `json:"cached_input_tokens"`
			Output int64 `json:"output_tokens"`
		} `json:"total_token_usage"`
	} `json:"info"`
}

type Result struct {
	Trace      model.Trace
	Cwd        string
	BaseCommit string
	RemoteURL  string
}

type item struct {
	step model.Step
	tool *model.ToolCall
	key  string
}

// Parse reads a Codex CLI rollout file. Unknown line types and fields are
// ignored; they produce warnings, never errors.
func Parse(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	res := &Result{}
	tr := &res.Trace
	var items []*item
	calls := map[string]*item{}
	var models []string
	var lastTs time.Time
	agentSID := ""
	sawUserItem := false
	var eventUsers []line
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	n := 0
	for sc.Scan() {
		n++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			tr.Warnings = append(tr.Warnings, fmt.Sprintf("line %d: %v", n, err))
			continue
		}
		if !l.Timestamp.IsZero() {
			lastTs = l.Timestamp
		}
		key := fmt.Sprintf("%d", n)
		switch l.Type {
		case "session_meta":
			var m sessionMeta
			_ = json.Unmarshal(l.Payload, &m)
			if m.Meta != nil {
				g := m.Git
				m = *m.Meta
				if m.Git == nil {
					m.Git = g
				}
			}
			agentSID, res.Cwd = m.ID, m.Cwd
			tr.Session.AgentVersion = m.CLIVersion
			if m.Git != nil {
				res.BaseCommit, res.RemoteURL = m.Git.CommitHash, m.Git.RepositoryURL
			}
			if tr.Session.StartedAt.IsZero() {
				tr.Session.StartedAt = l.Timestamp
			}
		case "turn_context":
			var tc turnContext
			_ = json.Unmarshal(l.Payload, &tc)
			if tc.Model != "" && !contains(models, tc.Model) {
				models = append(models, tc.Model)
			}
			if res.Cwd == "" {
				res.Cwd = tc.Cwd
			}
		case "event_msg":
			var e eventMsg
			_ = json.Unmarshal(l.Payload, &e)
			switch e.Type {
			case "user_message":
				eventUsers = append(eventUsers, l)
			case "token_count":
				if e.Info != nil && e.Info.Total != nil {
					tr.Session.InputTokens, tr.Session.OutputTokens = e.Info.Total.Input, e.Info.Total.Output
				}
			}
		case "response_item":
			var ri responseItem
			if err := json.Unmarshal(l.Payload, &ri); err != nil {
				tr.Warnings = append(tr.Warnings, fmt.Sprintf("line %d: %v", n, err))
				continue
			}
			switch ri.Type {
			case "message":
				text := joinText(ri.Content)
				switch ri.Role {
				case "user":
					if skipUserText(text) {
						continue
					}
					sawUserItem = true
					items = append(items, &item{key: key, step: model.Step{TsStart: l.Timestamp, Kind: model.StepUserMessage, Text: text, ActorID: "human"}})
				case "assistant":
					if strings.TrimSpace(text) != "" {
						items = append(items, &item{key: key, step: model.Step{TsStart: l.Timestamp, Kind: model.StepAssistantMessage, Text: text, ActorID: "agent"}})
					}
				}
			case "reasoning":
				var parts []string
				for _, s := range ri.Summary {
					if s.Text != "" {
						parts = append(parts, s.Text)
					}
				}
				if len(parts) > 0 {
					items = append(items, &item{key: key, step: model.Step{TsStart: l.Timestamp, Kind: model.StepReasoning, Text: strings.Join(parts, "\n"), ActorID: "agent"}})
				}
			case "function_call", "custom_tool_call", "local_shell_call":
				input := ri.Arguments
				if ri.Type == "custom_tool_call" {
					b, _ := json.Marshal(map[string]string{"input": ri.Input})
					input = string(b)
				}
				name := ri.Name
				if ri.Type == "local_shell_call" && ri.Action != nil {
					name = "shell"
					b, _ := json.Marshal(map[string]any{"command": ri.Action.Command})
					input = string(b)
				}
				tc := &model.ToolCall{ToolUseID: ri.CallID, ToolName: name, Input: input, OK: true}
				it := &item{key: key, tool: tc, step: model.Step{TsStart: l.Timestamp, Kind: model.StepToolCall, ActorID: "agent"}}
				items = append(items, it)
				if ri.CallID != "" {
					calls[ri.CallID] = it
				}
			case "function_call_output", "custom_tool_call_output":
				it, ok := calls[ri.CallID]
				if !ok {
					continue
				}
				out, code := outputText(ri.Output)
				it.tool.Output = out
				it.tool.ExitCode = code
				if code != nil && *code != 0 {
					it.tool.OK = false
				}
				it.step.TsEnd = l.Timestamp
			}
		default:
			if l.Type != "compacted" {
				tr.Warnings = append(tr.Warnings, fmt.Sprintf("line %d: unknown type %q", n, l.Type))
			}
		}
	}
	if err := sc.Err(); err != nil {
		tr.Warnings = append(tr.Warnings, err.Error())
	}
	if !sawUserItem {
		for i, e := range eventUsers {
			var m eventMsg
			_ = json.Unmarshal(e.Payload, &m)
			if !skipUserText(m.Message) {
				items = append(items, &item{key: fmt.Sprintf("u%d", i), step: model.Step{TsStart: e.Timestamp, Kind: model.StepUserMessage, Text: m.Message, ActorID: "human"}})
			}
		}
		sortItems(items)
	}
	if agentSID == "" {
		agentSID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	if tr.Session.StartedAt.IsZero() && len(items) > 0 {
		tr.Session.StartedAt = items[0].step.TsStart
	}
	s := &tr.Session
	s.ID = model.StableID(s.StartedAt, Agent, agentSID)
	s.Agent, s.AgentSessionID, s.Models, s.Fidelity, s.Cwd = Agent, agentSID, models, model.FidelityImported, res.Cwd
	s.EndedAt, s.Status = lastTs, "imported"
	agentActor := model.StableID(s.StartedAt, s.ID, "actor", "agent")
	humanActor := model.StableID(s.StartedAt, s.ID, "actor", "human")
	tr.Actors = []model.Actor{{ID: agentActor, SessionID: s.ID, Kind: "agent", Name: Agent}, {ID: humanActor, SessionID: s.ID, Kind: "human", Name: "human"}}
	for i, it := range items {
		st := it.step
		st.ID = model.StableID(st.TsStart, s.ID, it.key)
		st.SessionID, st.Seq = s.ID, i+1
		if st.ActorID == "human" {
			st.ActorID = humanActor
		} else {
			st.ActorID = agentActor
		}
		if it.tool != nil {
			tc := *it.tool
			tc.StepID = st.ID
			tc.Category = Category(tc.ToolName)
			st.Text = Summary(tc.ToolName, tc.Input)
			tr.ToolCalls = append(tr.ToolCalls, tc)
			if patch := PatchOf(tc.ToolName, tc.Input); patch != "" {
				for j, fe := range ParsePatch(patch) {
					tr.Edits = append(tr.Edits, model.Edit{ID: model.StableID(st.TsStart, st.ID, fe.Path, strconv.Itoa(j)), StepID: st.ID,
						FilePath: fe.Path, OldPath: fe.OldPath, Patch: fe.Patch, Added: fe.Added, Removed: fe.Removed})
				}
			}
		}
		tr.Steps = append(tr.Steps, st)
	}
	return res, nil
}

func sortItems(items []*item) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].step.TsStart.Before(items[j-1].step.TsStart); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func skipUserText(t string) bool {
	t = strings.TrimSpace(t)
	return t == "" || strings.HasPrefix(t, "<environment_context>") || strings.HasPrefix(t, "<user_instructions>") ||
		strings.HasPrefix(t, "# AGENTS.md") || strings.HasPrefix(t, "<permissions") || strings.HasPrefix(t, "<INSTRUCTIONS>")
}

func joinText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var items []contentItem
	_ = json.Unmarshal(raw, &items)
	var parts []string
	for _, c := range items {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

var exitRe = regexp.MustCompile(`(?m)^(?:Exit code:|Process exited with code)\s*(-?\d+)`)

// outputText handles the output shapes of Codex versions: a plain string, a
// JSON string with {"output","metadata":{"exit_code"}}, or a list of content items.
func outputText(raw json.RawMessage) (string, *int) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var items []contentItem
		if json.Unmarshal(raw, &items) == nil {
			var parts []string
			for _, c := range items {
				parts = append(parts, c.Text)
			}
			s = strings.Join(parts, "\n")
		} else {
			s = string(raw)
		}
	}
	var wrapped struct {
		Output   string `json:"output"`
		Metadata *struct {
			ExitCode *int `json:"exit_code"`
		} `json:"metadata"`
	}
	if strings.HasPrefix(strings.TrimSpace(s), "{") && json.Unmarshal([]byte(s), &wrapped) == nil && (wrapped.Output != "" || wrapped.Metadata != nil) {
		var code *int
		if wrapped.Metadata != nil {
			code = wrapped.Metadata.ExitCode
		}
		return wrapped.Output, code
	}
	if m := exitRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return s, &n
	}
	return s, nil
}

func Category(tool string) model.ToolCategory {
	switch tool {
	case "shell", "exec_command", "local_shell", "container.exec", "unified_exec", "write_stdin":
		return model.CatShell
	case "apply_patch":
		return model.CatEdit
	case "read_file", "view_image":
		return model.CatRead
	case "web_search", "web_fetch":
		return model.CatWeb
	case "update_plan":
		return model.CatOther
	}
	if strings.HasPrefix(tool, "mcp__") || strings.Contains(tool, ".") {
		return model.CatMCP
	}
	return model.CatOther
}

type args struct {
	Command any    `json:"command"`
	Cmd     string `json:"cmd"`
	Input   string `json:"input"`
	Patch   string `json:"patch"`
	Workdir string `json:"workdir"`
}

func Command(_ string, input string) string { return adapters.Command(input) }

func PatchOf(tool, input string) string {
	var a args
	_ = json.Unmarshal([]byte(input), &a)
	switch {
	case tool == "apply_patch" && a.Input != "":
		return a.Input
	case tool == "apply_patch" && a.Patch != "":
		return a.Patch
	}
	if cmd := Command(tool, input); strings.Contains(cmd, "*** Begin Patch") {
		return cmd[strings.Index(cmd, "*** Begin Patch"):]
	}
	return ""
}

func Summary(tool, input string) string {
	if p := PatchOf(tool, input); p != "" {
		var files []string
		for _, fe := range ParsePatch(p) {
			files = append(files, fe.Path)
		}
		return "apply_patch " + strings.Join(files, ", ")
	}
	if cmd := Command(tool, input); cmd != "" {
		first, _, _ := strings.Cut(strings.TrimSpace(cmd), "\n")
		return tool + ": " + first
	}
	return tool
}

type FileEdit struct {
	Path, OldPath, Patch string
	Added, Removed       int
	Deleted, Created     bool
}

// ParsePatch parses the apply_patch format ("*** Begin Patch" ... "*** End Patch").
func ParsePatch(p string) []FileEdit {
	var out []FileEdit
	var cur *FileEdit
	var body []string
	flush := func() {
		if cur != nil {
			cur.Patch = strings.Join(body, "\n")
			out = append(out, *cur)
		}
		cur, body = nil, nil
	}
	for _, l := range strings.Split(p, "\n") {
		switch {
		case strings.HasPrefix(l, "*** Update File: "):
			flush()
			cur = &FileEdit{Path: strings.TrimSpace(strings.TrimPrefix(l, "*** Update File: "))}
		case strings.HasPrefix(l, "*** Add File: "):
			flush()
			cur = &FileEdit{Path: strings.TrimSpace(strings.TrimPrefix(l, "*** Add File: ")), Created: true}
		case strings.HasPrefix(l, "*** Delete File: "):
			flush()
			cur = &FileEdit{Path: strings.TrimSpace(strings.TrimPrefix(l, "*** Delete File: ")), Deleted: true}
		case strings.HasPrefix(l, "*** Move to: ") && cur != nil:
			cur.OldPath = cur.Path
			cur.Path = strings.TrimSpace(strings.TrimPrefix(l, "*** Move to: "))
		case strings.HasPrefix(l, "*** End Patch"), strings.HasPrefix(l, "*** Begin Patch"), strings.HasPrefix(l, "*** End of File"):
		case cur != nil:
			body = append(body, l)
			if strings.HasPrefix(l, "+") {
				cur.Added++
			} else if strings.HasPrefix(l, "-") {
				cur.Removed++
			}
		}
	}
	flush()
	return out
}

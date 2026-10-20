package claudecode

import (
	"encoding/json"
	"path"
	"strings"
)

const Agent = "claude-code"

const (
	EvSessionStart       = "SessionStart"
	EvUserPromptSubmit   = "UserPromptSubmit"
	EvPreToolUse         = "PreToolUse"
	EvPostToolUse        = "PostToolUse"
	EvPostToolUseFailure = "PostToolUseFailure"
	EvSubagentStart      = "SubagentStart"
	EvSubagentStop       = "SubagentStop"
	EvPreCompact         = "PreCompact"
	EvStop               = "Stop"
	EvSessionEnd         = "SessionEnd"
)

var Events = []string{
	EvSessionStart, EvUserPromptSubmit, EvPreToolUse, EvPostToolUse, EvPostToolUseFailure,
	EvSubagentStart, EvSubagentStop, EvPreCompact, EvStop, EvSessionEnd,
}

func IsToolEvent(ev string) bool {
	return ev == EvPreToolUse || ev == EvPostToolUse || ev == EvPostToolUseFailure
}

type HookPayload struct {
	SessionID            string          `json:"session_id"`
	TranscriptPath       string          `json:"transcript_path"`
	Cwd                  string          `json:"cwd"`
	HookEventName        string          `json:"hook_event_name"`
	PermissionMode       string          `json:"permission_mode"`
	PromptID             string          `json:"prompt_id"`
	Prompt               string          `json:"prompt"`
	Source               string          `json:"source"`
	Reason               string          `json:"reason"`
	AgentID              string          `json:"agent_id"`
	AgentType            string          `json:"agent_type"`
	AgentTranscriptPath  string          `json:"agent_transcript_path"`
	ToolName             string          `json:"tool_name"`
	ToolInput            json.RawMessage `json:"tool_input"`
	ToolResponse         json.RawMessage `json:"tool_response"`
	ToolUseID            string          `json:"tool_use_id"`
	Error                string          `json:"error"`
	IsInterrupt          bool            `json:"is_interrupt"`
	DurationMS           int64           `json:"duration_ms"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	Trigger              string          `json:"trigger"`
	Model                string          `json:"model"`
}

func ParseHookPayload(b []byte) (HookPayload, error) {
	var p HookPayload
	err := json.Unmarshal(b, &p)
	return p, err
}

func MatchTool(patterns []string, tool string) bool {
	for _, p := range patterns {
		if p == tool {
			return true
		}
		if strings.ContainsAny(p, "*?[") {
			if ok, _ := path.Match(p, tool); ok {
				return true
			}
		}
	}
	return false
}

func IsMutatingTool(mutating, readonly []string, tool string) bool {
	if MatchTool(readonly, tool) {
		return false
	}
	if MatchTool(mutating, tool) {
		return true
	}
	return strings.HasPrefix(tool, "mcp__")
}

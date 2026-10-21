package adapters

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Command returns the shell command in a tool input of any agent: a string
// "command" (Claude Code), a "cmd" string, or a command array such as
// ["bash", "-lc", "<script>"] (Codex), where the script is the command.
func Command(input string) string {
	var a struct {
		Command any    `json:"command"`
		Cmd     string `json:"cmd"`
	}
	if json.Unmarshal([]byte(input), &a) != nil {
		return ""
	}
	if a.Cmd != "" {
		return a.Cmd
	}
	switch c := a.Command.(type) {
	case string:
		return c
	case []any:
		parts := make([]string, 0, len(c))
		for _, p := range c {
			parts = append(parts, fmt.Sprint(p))
		}
		if len(parts) == 3 && (parts[0] == "bash" || parts[0] == "sh" || parts[0] == "zsh") && strings.HasPrefix(parts[1], "-") {
			return parts[2]
		}
		return strings.Join(parts, " ")
	}
	return ""
}

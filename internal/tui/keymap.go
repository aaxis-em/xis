package tui

import (
	"fmt"
	"sort"
	"strings"
)

var actions = map[string]string{
	"down": "j", "up": "k", "pane": "tab", "expand": "enter", "understood": "u", "question": "x",
	"timeline": "t", "flags": "f", "ask": "a", "editor": "o", "help": "?", "quit": "q", "search": "/",
	"next_flag": "n", "prev_flag": "N", "next_prompt": "p", "prev_prompt": "P", "full_prompt": "i", "top": "g", "bottom": "G", "prev_unit": "[", "next_unit": "]",
}

func Actions() []string {
	out := make([]string, 0, len(actions))
	for a := range actions {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// SetKeymap maps extra keys to actions, for example {"quit": "ctrl+q"}. The
// default keys keep working.
func (m *Model) SetKeymap(overrides map[string]string) error {
	m.keys = map[string]string{}
	for action, key := range overrides {
		def, ok := actions[strings.ToLower(action)]
		if !ok {
			return fmt.Errorf("ui.keymap: unknown action %q (actions: %s)", action, strings.Join(Actions(), ", "))
		}
		m.keys[key] = def
	}
	return nil
}

func (m *Model) translate(key string) string {
	if def, ok := m.keys[key]; ok {
		return def
	}
	return key
}

// SetLightTheme uses a syntax style for light terminals.
func (m *Model) SetLightTheme() {
	m.mdStyle = "light"
	m.hl.style = "github"
	m.hl.cache = map[string][]string{}
}

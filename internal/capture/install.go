package capture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
)

const hookMarker = "xis hook claude-code"

func HookCommand(xisCmd, event string) string {
	return fmt.Sprintf("%s hook claude-code %s", xisCmd, event)
}

func hookTimeout(event string) int {
	if event == claudecode.EvSessionEnd {
		return 1
	}
	return 10
}

// IsHome reports whether dir is the home directory. A git repository there
// holds all files of the user, so xis neither installs project hooks for
// it nor takes snapshots of it.
func IsHome(dir string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || dir == "" {
		return false
	}
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	return filepath.Clean(dir) == filepath.Clean(home)
}

func SettingsPath(scope, repoDir, home string) (string, error) {
	switch scope {
	case "", "project":
		if repoDir == "" {
			return "", fmt.Errorf("project scope needs a git repository")
		}
		return filepath.Join(repoDir, ".claude", "settings.json"), nil
	case "user":
		return filepath.Join(home, ".claude", "settings.json"), nil
	}
	return "", fmt.Errorf("unknown scope %q (use project or user)", scope)
}

func readSettings(path string) (*object, os.FileMode, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return newObject(), 0o644, nil
	}
	if err != nil {
		return nil, 0, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	o, err := parseOrdered(b)
	if err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return o, fi.Mode().Perm(), nil
}

func writeSettings(path string, o *object, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".xis-tmp"
	if err := os.WriteFile(tmp, encodeOrdered(o), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func isXisEntry(v any) bool {
	o, ok := v.(*object)
	if !ok {
		return false
	}
	c, _ := o.get("command")
	s, _ := c.(string)
	return strings.Contains(s, hookMarker)
}

func groupHooks(g any) []any {
	o, ok := g.(*object)
	if !ok {
		return nil
	}
	h, _ := o.get("hooks")
	arr, _ := h.([]any)
	return arr
}

func newEntry(cmd string, timeout int) *object {
	e := newObject()
	e.set("type", "command")
	e.set("command", cmd)
	e.set("timeout", json.Number(fmt.Sprint(timeout)))
	return e
}

// Install merges the xis hooks into a Claude Code settings file. It keeps all
// other settings and hooks, and it does not add an entry twice.
func Install(path, xisCmd string) ([]string, error) {
	root, mode, err := readSettings(path)
	if err != nil {
		return nil, err
	}
	hv, _ := root.get("hooks")
	hooks, ok := hv.(*object)
	if hv != nil && !ok {
		return nil, fmt.Errorf("%s: \"hooks\" is not an object", path)
	}
	if hooks == nil {
		hooks = newObject()
	}
	var changes []string
	for _, ev := range claudecode.Events {
		want := HookCommand(xisCmd, ev)
		timeout := hookTimeout(ev)
		gv, _ := hooks.get(ev)
		groups, _ := gv.([]any)
		found := false
		for gi := range groups {
			arr := groupHooks(groups[gi])
			kept := arr[:0]
			for _, e := range arr {
				if !isXisEntry(e) {
					kept = append(kept, e)
					continue
				}
				if found {
					changes = append(changes, fmt.Sprintf("removed duplicate %s hook", ev))
					continue
				}
				found = true
				eo := e.(*object)
				c, _ := eo.get("command")
				t, _ := eo.get("timeout")
				if c != want || fmt.Sprint(t) != fmt.Sprint(timeout) {
					eo.set("command", want)
					eo.set("timeout", json.Number(fmt.Sprint(timeout)))
					changes = append(changes, fmt.Sprintf("updated %s hook: %s", ev, want))
				}
				kept = append(kept, e)
			}
			if g, ok := groups[gi].(*object); ok {
				g.set("hooks", kept)
			}
		}
		if !found {
			g := newObject()
			if claudecode.IsToolEvent(ev) {
				g.set("matcher", "*")
			}
			g.set("hooks", []any{newEntry(want, timeout)})
			groups = append(groups, g)
			changes = append(changes, fmt.Sprintf("added %s hook: %s", ev, want))
		}
		hooks.set(ev, groups)
	}
	if len(changes) == 0 {
		return nil, nil
	}
	root.set("hooks", hooks)
	return changes, writeSettings(path, root, mode)
}

func Uninstall(path string) ([]string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	root, mode, err := readSettings(path)
	if err != nil {
		return nil, err
	}
	hv, _ := root.get("hooks")
	hooks, ok := hv.(*object)
	if !ok {
		return nil, nil
	}
	var changes []string
	for _, ev := range append([]string{}, hooks.keys...) {
		gv, _ := hooks.get(ev)
		groups, ok := gv.([]any)
		if !ok {
			continue
		}
		var keptGroups []any
		for _, g := range groups {
			arr := groupHooks(g)
			var kept []any
			removed := false
			for _, e := range arr {
				if isXisEntry(e) {
					removed = true
					changes = append(changes, fmt.Sprintf("removed %s hook", ev))
					continue
				}
				kept = append(kept, e)
			}
			if removed && len(kept) == 0 {
				continue
			}
			if gobj, ok := g.(*object); ok && removed {
				gobj.set("hooks", kept)
			}
			keptGroups = append(keptGroups, g)
		}
		if len(keptGroups) == 0 {
			hooks.del(ev)
		} else {
			hooks.set(ev, keptGroups)
		}
	}
	if len(changes) == 0 {
		return nil, nil
	}
	if len(hooks.keys) == 0 {
		root.del("hooks")
	}
	return changes, writeSettings(path, root, mode)
}

// Installed reports which events have an xis hook in the settings file.
func Installed(path string) (map[string]bool, error) {
	out := map[string]bool{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	root, err := parseOrdered(b)
	if err != nil {
		return nil, err
	}
	hv, _ := root.get("hooks")
	hooks, ok := hv.(*object)
	if !ok {
		return out, nil
	}
	for _, ev := range hooks.keys {
		gv, _ := hooks.get(ev)
		groups, _ := gv.([]any)
		for _, g := range groups {
			for _, e := range groupHooks(g) {
				if isXisEntry(e) {
					out[ev] = true
				}
			}
		}
	}
	return out, nil
}

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Capture   Capture   `toml:"capture"`
	LLM       LLM       `toml:"llm"`
	Rules     Rules     `toml:"rules"`
	Scope     Scope     `toml:"scope"`
	Redact    Redact    `toml:"redact"`
	Retention Retention `toml:"retention"`
	Notes     Notes     `toml:"notes"`
	UI        UI        `toml:"ui"`
}

type Capture struct {
	Precise        bool     `toml:"precise"`
	Exclude        []string `toml:"exclude"`
	MutatingTools  []string `toml:"mutating_tools"`
	ReadonlyTools  []string `toml:"readonly_tools"`
	MaxFileBytes   int64    `toml:"max_file_bytes"`
	MaxOutputBytes int      `toml:"max_output_bytes"`
	TestCommands   []string `toml:"test_commands"`
	TestGlobs      []string `toml:"test_globs"`
}

type LLM struct {
	Provider           string  `toml:"provider"`
	Model              string  `toml:"model"`
	BaseURL            string  `toml:"base_url"`
	APIKeyEnv          string  `toml:"api_key_env"`
	MaxUSDPerChange    float64 `toml:"max_usd_per_change"`
	SendCode           bool    `toml:"send_code"`
	PriceInputPerMTok  float64 `toml:"price_input_per_mtok"`
	PriceOutputPerMTok float64 `toml:"price_output_per_mtok"`
	MaxTokens          int     `toml:"max_tokens"`
}

type Rules struct {
	Disabled         []string          `toml:"disabled"`
	Severity         map[string]string `toml:"severity"`
	LargeChangeLines int               `toml:"large_change_lines"`
	LargeChangeFiles int               `toml:"large_change_files"`
	RetryLoopRuns    int               `toml:"retry_loop_runs"`
}

type Scope struct {
	Paths []string `toml:"paths"`
}

type Redact struct {
	Patterns         []string `toml:"patterns"`
	EntropyThreshold float64  `toml:"entropy_threshold"`
	EntropyMinLength int      `toml:"entropy_min_length"`
}

type Retention struct {
	SnapshotsDays int `toml:"snapshots_days"`
	RawDays       int `toml:"raw_days"`
}

type Notes struct {
	Auto bool `toml:"auto"`
}

type UI struct {
	Theme  string            `toml:"theme"`
	Keymap map[string]string `toml:"keymap"`
}

func Default() Config {
	return Config{
		Capture: Capture{
			Precise:        false,
			Exclude:        []string{".env*", "*.pem", "*.key", "id_rsa*", "*.p12", "node_modules/", "vendor/", "dist/", "build/", "target/", ".venv/", "__pycache__/", ".next/"},
			MutatingTools:  []string{"Edit", "Write", "MultiEdit", "NotebookEdit", "Bash", "mcp__*"},
			ReadonlyTools:  []string{"Read", "Grep", "Glob", "LS", "WebFetch", "WebSearch", "TodoWrite", "Task", "Agent", "ExitPlanMode", "BashOutput", "KillShell", "ToolSearch", "Skill"},
			MaxFileBytes:   1 << 20,
			MaxOutputBytes: 32 << 10,
			TestCommands:   []string{},
			TestGlobs:      []string{},
		},
		LLM: LLM{
			Provider:           "auto",
			Model:              "claude-sonnet-5-5",
			APIKeyEnv:          "ANTHROPIC_API_KEY",
			MaxUSDPerChange:    0.50,
			SendCode:           true,
			PriceInputPerMTok:  3.0,
			PriceOutputPerMTok: 15.0,
			MaxTokens:          4096,
		},
		Rules: Rules{
			Disabled:         []string{},
			Severity:         map[string]string{},
			LargeChangeLines: 500,
			LargeChangeFiles: 20,
			RetryLoopRuns:    4,
		},
		Redact: Redact{
			Patterns:         []string{},
			EntropyThreshold: 4.5,
			EntropyMinLength: 20,
		},
		Retention: Retention{SnapshotsDays: 30, RawDays: 7},
		UI:        UI{Theme: "auto", Keymap: map[string]string{}},
	}
}

func Load(p Paths, repoDir string) (Config, error) {
	c := Default()
	if err := mergeFile(&c, p.ConfigFile); err != nil {
		return c, err
	}
	if repoDir != "" {
		if err := mergeFile(&c, filepath.Join(repoDir, ".xis.toml")); err != nil {
			return c, err
		}
	}
	if err := applyEnv(&c, os.Environ()); err != nil {
		return c, err
	}
	return c, nil
}

func mergeFile(c *Config, path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	if _, err := toml.DecodeFile(path, c); err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	return nil
}

func (l LLM) EffectiveProvider() string {
	p := strings.ToLower(strings.TrimSpace(l.Provider))
	if p == "" || p == "auto" {
		if os.Getenv(l.APIKeyEnv) != "" {
			return "anthropic"
		}
		return "none"
	}
	return p
}

type field struct {
	Key   string
	Value reflect.Value
}

func fields(c *Config) []field {
	var out []field
	rv := reflect.ValueOf(c).Elem()
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		sec := rt.Field(i).Tag.Get("toml")
		sv := rv.Field(i)
		st := sv.Type()
		for j := 0; j < st.NumField(); j++ {
			out = append(out, field{Key: sec + "." + st.Field(j).Tag.Get("toml"), Value: sv.Field(j)})
		}
	}
	return out
}

func Keys() []string {
	c := Default()
	var ks []string
	for _, f := range fields(&c) {
		ks = append(ks, f.Key)
	}
	sort.Strings(ks)
	return ks
}

func (c Config) Get(key string) (string, error) {
	for _, f := range fields(&c) {
		if f.Key == key {
			return formatValue(f.Value), nil
		}
	}
	return "", fmt.Errorf("unknown config key %q", key)
}

func formatValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = fmt.Sprint(v.Index(i).Interface())
		}
		return strings.Join(parts, ",")
	case reflect.Map:
		keys := v.MapKeys()
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%v=%v", k.Interface(), v.MapIndex(k).Interface()))
		}
		sort.Strings(parts)
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(v.Interface())
	}
}

func parseValue(target reflect.Value, s string) (any, error) {
	switch target.Kind() {
	case reflect.String:
		return s, nil
	case reflect.Bool:
		return strconv.ParseBool(s)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		return n, err
	case reflect.Float64:
		return strconv.ParseFloat(s, 64)
	case reflect.Slice:
		out := []string{}
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	case reflect.Map:
		out := map[string]string{}
		for _, p := range strings.Split(s, ",") {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if !ok {
				if p == "" {
					continue
				}
				return nil, fmt.Errorf("want key=value, got %q", p)
			}
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported type %s", target.Kind())
}

func assign(target reflect.Value, v any) {
	switch target.Kind() {
	case reflect.Int, reflect.Int64:
		target.SetInt(v.(int64))
	default:
		target.Set(reflect.ValueOf(v))
	}
}

func applyEnv(c *Config, environ []string) error {
	env := map[string]string{}
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "XIS_") {
			env[k] = v
		}
	}
	for _, f := range fields(c) {
		name := "XIS_" + strings.ToUpper(strings.ReplaceAll(f.Key, ".", "_"))
		s, ok := env[name]
		if !ok {
			continue
		}
		v, err := parseValue(f.Value, s)
		if err != nil {
			return fmt.Errorf("env %s: %w", name, err)
		}
		assign(f.Value, v)
	}
	return nil
}

func SetInFile(path, key, value string) error {
	def := Default()
	var target reflect.Value
	for _, f := range fields(&def) {
		if f.Key == key {
			target = f.Value
		}
	}
	if !target.IsValid() {
		return fmt.Errorf("unknown config key %q", key)
	}
	v, err := parseValue(target, value)
	if err != nil {
		return fmt.Errorf("value for %s: %w", key, err)
	}
	doc := map[string]any{}
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &doc); err != nil {
			return fmt.Errorf("read config %s: %w", path, err)
		}
	}
	sec, name, _ := strings.Cut(key, ".")
	m, _ := doc[sec].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	m[name] = v
	doc[sec] = m
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(doc); err != nil {
		return err
	}
	return WriteFile(path, []byte(b.String()))
}

func (c Config) Validate() error {
	switch c.LLM.EffectiveProvider() {
	case "anthropic", "openaicompat", "none", "mock":
	default:
		return fmt.Errorf("llm.provider: unknown provider %q (use anthropic, openaicompat or none)", c.LLM.Provider)
	}
	switch c.UI.Theme {
	case "", "auto", "dark", "light", "none":
	default:
		return fmt.Errorf("ui.theme: unknown theme %q (use auto, dark, light or none)", c.UI.Theme)
	}
	for id, sev := range c.Rules.Severity {
		switch sev {
		case "info", "low", "medium", "high":
		default:
			return fmt.Errorf("rules.severity.%s: unknown severity %q", id, sev)
		}
	}
	return nil
}

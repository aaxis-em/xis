package claudecode

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/testdetect"
)

type Input struct {
	SessionID     string
	Events        []model.HookEvent
	Transcript    []Entry
	Subagents     map[string][]Entry
	HasTranscript bool
}

type item struct {
	ts        time.Time
	order     int
	key       string
	kind      model.StepKind
	text      string
	actor     string
	tool      *model.ToolCall
	tsEnd     time.Time
	snapshot  string
	toolUseID string
	id        string
}

type builder struct {
	in        Input
	items     []*item
	tools     map[string]*item
	results   map[string]Block
	prompts   []*item
	models    []string
	usage     map[string]Usage
	actors    map[string]bool
	agentType map[string]string
	order     int
	title     string
	version   string
	cwd       string
	warnings  []string
}

const (
	actorAgent = "agent"
	actorHuman = "human"
)

func subagentActor(agentID string) string { return "subagent:" + agentID }

func (b *builder) add(it *item) *item {
	b.order++
	it.order = b.order
	b.items = append(b.items, it)
	return it
}

func Build(in Input) model.Trace {
	b := &builder{
		in:        in,
		tools:     map[string]*item{},
		results:   map[string]Block{},
		usage:     map[string]Usage{},
		actors:    map[string]bool{},
		agentType: map[string]string{},
	}
	b.transcript(in.Transcript, "")
	agentIDs := make([]string, 0, len(in.Subagents))
	for id := range in.Subagents {
		agentIDs = append(agentIDs, id)
	}
	sort.Strings(agentIDs)
	for _, id := range agentIDs {
		b.actors[id] = true
		b.transcript(in.Subagents[id], id)
	}
	for id, res := range b.results {
		if it, ok := b.tools[id]; ok {
			applyResult(it, res)
		}
	}
	marks := b.hooks()
	return b.finish(marks)
}

func skipPrompt(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	for _, p := range []string{"<command-", "<local-command", "<task-notification", "<system-reminder", "Caveat:", "[Request interrupted"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

func (b *builder) transcript(entries []Entry, agentID string) {
	actor := actorAgent
	if agentID != "" {
		actor = subagentActor(agentID)
	}
	for _, e := range entries {
		if b.version == "" && e.Version != "" {
			b.version = e.Version
		}
		if b.cwd == "" && e.Cwd != "" {
			b.cwd = e.Cwd
		}
		switch e.Type {
		case "ai-title":
			if e.AITitle != "" {
				b.title = e.AITitle
			}
		case "system":
			if e.Subtype == "compact_boundary" {
				b.add(&item{ts: e.Timestamp, key: e.UUID, kind: model.StepCompaction, text: "Context compaction", actor: actor})
			}
		case "user":
			if e.Message == nil {
				continue
			}
			hasResult := false
			for _, blk := range e.Message.Content {
				if blk.Type == "tool_result" {
					hasResult = true
					b.results[blk.ToolUseID] = blk
				}
			}
			if hasResult || agentID != "" || e.IsMeta || e.IsSidechain {
				continue
			}
			if e.Origin != nil && e.Origin.Kind != "" && e.Origin.Kind != "human" {
				continue
			}
			var parts []string
			for _, blk := range e.Message.Content {
				if blk.Type == "text" {
					parts = append(parts, blk.Text)
				}
			}
			text := strings.Join(parts, "\n")
			if skipPrompt(text) {
				continue
			}
			b.prompts = append(b.prompts, b.add(&item{ts: e.Timestamp, key: e.UUID, kind: model.StepUserMessage, text: text, actor: actorHuman}))
		case "assistant":
			if e.Message == nil {
				continue
			}
			if m := e.Message.Model; m != "" && m != "<synthetic>" && !contains(b.models, m) {
				b.models = append(b.models, m)
			}
			if e.Message.Usage != nil && e.Message.ID != "" {
				b.usage[e.Message.ID] = *e.Message.Usage
			}
			for i, blk := range e.Message.Content {
				key := fmt.Sprintf("%s:%d", e.UUID, i)
				switch blk.Type {
				case "text":
					if strings.TrimSpace(blk.Text) != "" {
						b.add(&item{ts: e.Timestamp, key: key, kind: model.StepAssistantMessage, text: blk.Text, actor: actor})
					}
				case "thinking":
					if strings.TrimSpace(blk.Thinking) != "" {
						b.add(&item{ts: e.Timestamp, key: key, kind: model.StepReasoning, text: blk.Thinking, actor: actor})
					}
				case "tool_use":
					tc := &model.ToolCall{ToolUseID: blk.ID, ToolName: blk.Name, Input: string(blk.Input), OK: true}
					it := b.add(&item{ts: e.Timestamp, key: "tool:" + blk.ID, kind: model.StepToolCall, actor: actor, tool: tc, toolUseID: blk.ID})
					b.tools[blk.ID] = it
					if blk.Name == "ExitPlanMode" {
						var in struct {
							Plan string `json:"plan"`
						}
						_ = json.Unmarshal(blk.Input, &in)
						if in.Plan != "" {
							b.add(&item{ts: e.Timestamp, key: key + ":plan", kind: model.StepPlan, text: in.Plan, actor: actor})
						}
					}
				}
			}
		}
	}
}

func applyResult(it *item, res Block) {
	it.tool.Output = ToolResultText(res.Content)
	if res.IsError {
		it.tool.OK = false
		if code := testdetect.ExitCodeFromText(it.tool.Output); code != nil {
			it.tool.ExitCode = code
		}
	} else if it.tool.ToolName == "Bash" && it.tool.ExitCode == nil {
		zero := 0
		it.tool.ExitCode = &zero
	}
}

type mark struct {
	commit string
	it     *item
	base   bool
}

func (b *builder) hooks() []mark {
	var marks []mark
	type promptHook struct {
		ev      model.HookEvent
		p       HookPayload
		matched *item
	}
	var promptHooks []*promptHook
	compactFromHooks := false
	for _, ev := range b.in.Events {
		p, err := ParseHookPayload([]byte(ev.Payload))
		if err != nil {
			b.warnings = append(b.warnings, fmt.Sprintf("hook event %s at %s: payload is not an object", ev.Event, ev.Ts.Format(time.RFC3339)))
			continue
		}
		if b.cwd == "" {
			b.cwd = p.Cwd
		}
		actor := actorAgent
		if p.AgentID != "" {
			actor = subagentActor(p.AgentID)
			b.actors[p.AgentID] = true
			if p.AgentType != "" {
				b.agentType[p.AgentID] = p.AgentType
			}
		}
		switch ev.Event {
		case EvSessionStart:
			if ev.Snapshot != "" {
				marks = append(marks, mark{commit: ev.Snapshot, base: true})
			}
		case EvUserPromptSubmit:
			ph := &promptHook{ev: ev, p: p}
			promptHooks = append(promptHooks, ph)
			if ev.Snapshot != "" {
				he := b.add(&item{ts: ev.Ts.Add(-time.Microsecond), key: "human:" + ev.Key, kind: model.StepHumanEdit,
					text: "Files changed outside the agent before this prompt", actor: actorHuman, snapshot: ev.Snapshot})
				marks = append(marks, mark{commit: ev.Snapshot, it: he})
			}
		case EvPreToolUse:
			it := b.toolItem(ev, p, actor)
			if ev.Snapshot != "" {
				he := b.add(&item{ts: it.ts.Add(-time.Microsecond), key: "pre:" + ev.Key, kind: model.StepHumanEdit,
					text: "Files changed outside the agent's steps", actor: actorHuman, snapshot: ev.Snapshot})
				marks = append(marks, mark{commit: ev.Snapshot, it: he})
			}
		case EvPostToolUse, EvPostToolUseFailure:
			it := b.toolItem(ev, p, actor)
			it.tsEnd = ev.Ts
			if p.DurationMS > 0 {
				it.tool.DurationMS = p.DurationMS
			}
			if ev.Event == EvPostToolUseFailure {
				it.tool.OK = false
				if it.tool.Output == "" {
					it.tool.Output = p.Error
				}
				if code := testdetect.ExitCodeFromText(p.Error); code != nil {
					it.tool.ExitCode = code
				}
			} else {
				it.tool.OK = true
				if it.tool.Output == "" {
					it.tool.Output = responseText(p.ToolResponse)
				}
				if p.ToolName == "Bash" && it.tool.ExitCode == nil && !interrupted(p.ToolResponse) {
					zero := 0
					it.tool.ExitCode = &zero
				}
			}
			if ev.Snapshot != "" {
				it.snapshot = ev.Snapshot
				marks = append(marks, mark{commit: ev.Snapshot, it: it})
			}
		case EvSubagentStop:
			if _, ok := b.in.Subagents[p.AgentID]; !ok && strings.TrimSpace(p.LastAssistantMessage) != "" {
				b.add(&item{ts: ev.Ts, key: "substop:" + ev.Key, kind: model.StepAssistantMessage, text: p.LastAssistantMessage, actor: actor})
			}
		case EvPreCompact:
			compactFromHooks = true
			b.add(&item{ts: ev.Ts, key: "compact:" + ev.Key, kind: model.StepCompaction, text: "Context compaction (" + p.Trigger + ")", actor: actorAgent})
		case EvStop:
			if !b.in.HasTranscript && strings.TrimSpace(p.LastAssistantMessage) != "" {
				b.add(&item{ts: ev.Ts, key: "stop:" + ev.Key, kind: model.StepAssistantMessage, text: p.LastAssistantMessage, actor: actorAgent})
			}
		}
	}
	if compactFromHooks {
		kept := b.items[:0]
		for _, it := range b.items {
			if it.kind == model.StepCompaction && !strings.HasPrefix(it.key, "compact:") {
				continue
			}
			kept = append(kept, it)
		}
		b.items = kept
	}
	used := map[*item]bool{}
	for _, ph := range promptHooks {
		want := strings.TrimSpace(ph.p.Prompt)
		for _, pi := range b.prompts {
			if !used[pi] && strings.TrimSpace(pi.text) == want {
				ph.matched, used[pi] = pi, true
				break
			}
		}
	}
	for _, ph := range promptHooks {
		if ph.matched != nil {
			continue
		}
		for _, pi := range b.prompts {
			if !used[pi] && !pi.ts.Before(ph.ev.Ts.Add(-time.Minute)) {
				ph.matched, used[pi] = pi, true
				break
			}
		}
		if ph.matched == nil && !skipPrompt(ph.p.Prompt) {
			ph.matched = b.add(&item{ts: ph.ev.Ts, key: "prompt:" + ph.ev.Key, kind: model.StepUserMessage, text: ph.p.Prompt, actor: actorHuman})
		}
	}
	for _, ph := range promptHooks {
		if ph.matched == nil || ph.ev.Snapshot == "" {
			continue
		}
		for _, it := range b.items {
			if it.key == "human:"+ph.ev.Key && ph.matched.ts.Before(it.ts) {
				it.ts = ph.matched.ts.Add(-time.Microsecond)
			}
		}
	}
	return marks
}

func (b *builder) toolItem(ev model.HookEvent, p HookPayload, actor string) *item {
	if it, ok := b.tools[p.ToolUseID]; ok && p.ToolUseID != "" {
		if it.tool.ToolName == "" {
			it.tool.ToolName = p.ToolName
		}
		if p.AgentID != "" {
			it.actor = actor
		}
		return it
	}
	id := p.ToolUseID
	if id == "" {
		id = ev.Key
	}
	tc := &model.ToolCall{ToolUseID: p.ToolUseID, ToolName: p.ToolName, Input: string(p.ToolInput), OK: true}
	it := b.add(&item{ts: ev.Ts, key: "hooktool:" + id, kind: model.StepToolCall, actor: actor, tool: tc, toolUseID: p.ToolUseID})
	if p.ToolUseID != "" {
		b.tools[p.ToolUseID] = it
	}
	return it
}

func interrupted(raw json.RawMessage) bool {
	var r struct {
		Interrupted bool `json:"interrupted"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.Interrupted
}

func responseText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var bash struct {
		Stdout *string `json:"stdout"`
		Stderr string  `json:"stderr"`
	}
	if err := json.Unmarshal(raw, &bash); err == nil && bash.Stdout != nil {
		out := *bash.Stdout
		if bash.Stderr != "" {
			out += "\n" + bash.Stderr
		}
		return out
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (b *builder) finish(marks []mark) model.Trace {
	var tr model.Trace
	tr.Warnings = b.warnings
	sort.SliceStable(b.items, func(i, j int) bool {
		if !b.items[i].ts.Equal(b.items[j].ts) {
			return b.items[i].ts.Before(b.items[j].ts)
		}
		return b.items[i].order < b.items[j].order
	})

	agentSID := ""
	var start, end time.Time
	ended := false
	for _, ev := range b.in.Events {
		if p, err := ParseHookPayload([]byte(ev.Payload)); err == nil && p.SessionID != "" && agentSID == "" {
			agentSID = p.SessionID
		}
		if start.IsZero() || ev.Ts.Before(start) {
			start = ev.Ts
		}
		if ev.Ts.After(end) {
			end = ev.Ts
		}
		if ev.Event == EvSessionEnd {
			ended = true
		}
	}
	for _, e := range b.in.Transcript {
		if agentSID == "" && e.SessionID != "" {
			agentSID = e.SessionID
		}
		if !e.Timestamp.IsZero() && (start.IsZero() || e.Timestamp.Before(start)) {
			start = e.Timestamp
		}
		if e.Timestamp.After(end) {
			end = e.Timestamp
		}
	}
	for _, it := range b.items {
		if start.IsZero() || it.ts.Before(start) {
			start = it.ts
		}
	}

	sessID := b.in.SessionID
	if sessID == "" {
		sessID = model.StableID(start, Agent, agentSID)
	}
	s := model.Session{
		ID: sessID, Agent: Agent, AgentVersion: b.version, AgentSessionID: agentSID, Models: b.models,
		Fidelity: model.FidelityLive, StartedAt: start.UTC(), EndedAt: end.UTC(), Cwd: b.cwd, Title: b.title,
	}
	for _, u := range b.usage {
		s.InputTokens += u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
		s.OutputTokens += u.OutputTokens
	}
	status := []string{"active"}
	if ended {
		status[0] = "ended"
	}
	if !b.in.HasTranscript {
		status = append(status, "no_transcript")
	}
	s.Status = strings.Join(status, ",")

	actorIDs := map[string]string{}
	actorID := func(key string) string {
		if id, ok := actorIDs[key]; ok {
			return id
		}
		id := model.StableID(start, sessID, "actor", key)
		actorIDs[key] = id
		return id
	}
	tr.Actors = append(tr.Actors,
		model.Actor{ID: actorID(actorAgent), SessionID: sessID, Kind: "agent", Name: Agent},
		model.Actor{ID: actorID(actorHuman), SessionID: sessID, Kind: "human", Name: "human"})
	sub := make([]string, 0, len(b.actors))
	for id := range b.actors {
		sub = append(sub, id)
	}
	sort.Strings(sub)
	for _, id := range sub {
		name := b.agentType[id]
		if name == "" {
			name = "subagent"
		}
		tr.Actors = append(tr.Actors, model.Actor{ID: actorID(subagentActor(id)), SessionID: sessID, Kind: "subagent", Name: name + " " + id, ParentActorID: actorID(actorAgent)})
	}

	var lastSpawn string
	firstSub := map[string]string{}
	for i, it := range b.items {
		it.id = model.StableID(it.ts, sessID, it.key)
		st := model.Step{ID: it.id, SessionID: sessID, ActorID: actorID(it.actor), Seq: i + 1, TsStart: it.ts.UTC(), Kind: it.kind, Text: it.text, Snapshot: it.snapshot}
		if !it.tsEnd.IsZero() {
			st.TsEnd = it.tsEnd.UTC()
		}
		if it.tool != nil {
			tc := *it.tool
			tc.StepID = it.id
			tc.Category = Category(tc.ToolName)
			if it.text == "" {
				st.Text = ToolSummary(tc.ToolName, tc.Input, b.cwd)
			}
			tr.ToolCalls = append(tr.ToolCalls, tc)
			if it.actor == actorAgent && (tc.ToolName == "Task" || tc.ToolName == "Agent") {
				lastSpawn = it.id
			}
		}
		if strings.HasPrefix(it.actor, "subagent:") {
			if _, ok := firstSub[it.actor]; !ok {
				firstSub[it.actor] = lastSpawn
			}
			st.ParentStepID = firstSub[it.actor]
		}
		tr.Steps = append(tr.Steps, st)
	}
	for _, m := range marks {
		sm := model.SnapshotMark{Commit: m.commit, Base: m.base}
		if m.it != nil {
			sm.StepID = m.it.id
		}
		tr.Snapshots = append(tr.Snapshots, sm)
	}
	for i := range tr.Snapshots {
		if tr.Snapshots[i].Base {
			s.BaseSnapshot = tr.Snapshots[i].Commit
			break
		}
	}
	if len(tr.Snapshots) > 0 {
		s.FinalSnapshot = tr.Snapshots[len(tr.Snapshots)-1].Commit
		if s.BaseSnapshot == "" {
			s.BaseSnapshot = tr.Snapshots[0].Commit
		}
	}
	tr.Session = s
	return tr
}

func Category(tool string) model.ToolCategory {
	switch tool {
	case "Read", "NotebookRead":
		return model.CatRead
	case "Grep", "Glob", "LS", "ToolSearch":
		return model.CatSearch
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return model.CatEdit
	case "Bash", "BashOutput", "KillShell":
		return model.CatShell
	case "WebFetch", "WebSearch":
		return model.CatWeb
	}
	if strings.HasPrefix(tool, "mcp__") {
		return model.CatMCP
	}
	return model.CatOther
}

type toolInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Path         string `json:"path"`
	Pattern      string `json:"pattern"`
	Command      string `json:"command"`
	Description  string `json:"description"`
	URL          string `json:"url"`
	Query        string `json:"query"`
	Prompt       string `json:"prompt"`
}

func ToolSummary(tool, input, cwd string) string {
	var in toolInput
	_ = json.Unmarshal([]byte(input), &in)
	rel := func(p string) string {
		if cwd != "" {
			if r, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(r, "..") {
				return r
			}
		}
		return p
	}
	switch {
	case in.Command != "":
		return tool + ": " + firstLine(in.Command)
	case in.FilePath != "":
		return tool + " " + rel(in.FilePath)
	case in.NotebookPath != "":
		return tool + " " + rel(in.NotebookPath)
	case in.Pattern != "":
		return tool + " " + in.Pattern
	case in.URL != "":
		return tool + " " + in.URL
	case in.Query != "":
		return tool + " " + in.Query
	case in.Description != "":
		return tool + ": " + in.Description
	case in.Path != "":
		return tool + " " + rel(in.Path)
	}
	return tool
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// ToolPaths returns the file paths that a tool call names in its input.
func ToolPaths(input string) []string {
	var in toolInput
	_ = json.Unmarshal([]byte(input), &in)
	var out []string
	for _, p := range []string{in.FilePath, in.NotebookPath, in.Path} {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/snapshot"
)

const maxPayloadBytes = 64 << 20

type Handler struct {
	Agent string
	Paths config.Paths
	Cfg   config.Config
	Now   func() time.Time
	Spawn func(sessionID string) error
}

func NewHandler(p config.Paths, c config.Config) *Handler {
	return &Handler{Paths: p, Cfg: c, Now: time.Now, Spawn: SpawnIngest}
}

// Handle must never fail the agent: it returns nothing, writes nothing to
// stdout and recovers from panics.
func (h *Handler) Handle(event string, stdin io.Reader) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("hook panic", "event", event, "panic", fmt.Sprint(r))
		}
	}()
	if err := h.handle(event, stdin); err != nil {
		slog.Error("hook failed", "event", event, "err", err.Error())
	}
}

func (h *Handler) handle(event string, stdin io.Reader) error {
	start := h.Now()
	raw, err := io.ReadAll(io.LimitReader(stdin, maxPayloadBytes))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	p, perr := claudecode.ParseHookPayload(raw)
	if perr != nil || !json.Valid(raw) {
		quoted, _ := json.Marshal(string(raw))
		raw = quoted
	}
	if event == "" {
		event = p.HookEventName
	}
	sid := p.SessionID
	if sid == "" {
		sid = "unknown"
	}

	line := Line{V: SpoolVersion, Agent: h.Agent, Ts: start.UTC(), Event: event, Payload: raw}
	lock, lerr := LockSession(h.Paths.Spool, sid, 3*time.Second)
	if lerr != nil {
		slog.Warn("hook lock", "event", event, "session", sid, "err", lerr.Error())
	}
	defer lock.Release()

	if h.wantsSnapshot(event, p.ToolName) {
		h.snapshot(&line, sid, event, p)
	}
	if err := AppendLine(h.Paths.Spool, sid, line); err != nil {
		return fmt.Errorf("spool: %w", err)
	}
	slog.Debug("hook", "event", event, "session", sid, "tool_use_id", p.ToolUseID, "snapshot", line.SnapshotID(), "ms", time.Since(start).Milliseconds())

	if event == claudecode.EvStop || event == claudecode.EvSessionEnd {
		lock.Release()
		if h.Spawn != nil {
			if err := h.Spawn(sid); err != nil {
				slog.Warn("spawn ingest", "session", sid, "err", err.Error())
			}
		}
	}
	return nil
}

func (h *Handler) wantsSnapshot(event, tool string) bool {
	switch event {
	case claudecode.EvSessionStart, claudecode.EvUserPromptSubmit:
		return true
	case claudecode.EvPostToolUse, claudecode.EvPostToolUseFailure:
		return claudecode.IsMutatingTool(h.Cfg.Capture.MutatingTools, h.Cfg.Capture.ReadonlyTools, tool)
	case claudecode.EvPreToolUse:
		return h.Cfg.Capture.Precise && claudecode.IsMutatingTool(h.Cfg.Capture.MutatingTools, h.Cfg.Capture.ReadonlyTools, tool)
	}
	return false
}

func (h *Handler) snapshot(line *Line, sid, event string, p claudecode.HookPayload) {
	cwd := p.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	repo, err := snapshot.Discover(cwd)
	if errors.Is(err, snapshot.ErrNotGit) {
		line.NoGit = true
		return
	}
	if err != nil {
		line.SnapshotError = firstLine(err.Error())
		return
	}
	if IsHome(repo.Top) {
		line.SnapshotError = "the git repository is the home directory; xis does not snapshot it"
		return
	}
	sh, err := snapshot.Open(h.Paths.Shadow, repo, h.Cfg.Capture.Exclude)
	if err != nil {
		line.SnapshotError = firstLine(err.Error())
		return
	}
	info := &RepoInfo{ID: repo.ID, Top: repo.Top, CommonDir: repo.CommonDir}
	if event == claudecode.EvSessionStart || sh.Head(sid) == "" {
		info.Head = repo.Head()
		info.Remote = repo.RemoteURL()
	}
	line.Repo = info
	label := event
	if p.ToolUseID != "" {
		label = event + " " + p.ToolUseID
	}
	commit, err := sh.Take(sid, label)
	if err != nil {
		line.SnapshotError = firstLine(err.Error())
		return
	}
	line.Snapshot = &commit
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

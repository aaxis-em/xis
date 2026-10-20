package model

import (
	"crypto/sha256"
	"encoding/binary"
	"time"

	"github.com/oklog/ulid/v2"
)

type Fidelity string

const (
	FidelityLive     Fidelity = "live"
	FidelityImported Fidelity = "imported"
)

type StepKind string

const (
	StepUserMessage      StepKind = "user_message"
	StepAssistantMessage StepKind = "assistant_message"
	StepReasoning        StepKind = "reasoning"
	StepPlan             StepKind = "plan"
	StepToolCall         StepKind = "tool_call"
	StepCompaction       StepKind = "compaction"
	StepHumanEdit        StepKind = "human_edit"
)

type ToolCategory string

const (
	CatRead   ToolCategory = "read"
	CatSearch ToolCategory = "search"
	CatEdit   ToolCategory = "edit"
	CatShell  ToolCategory = "shell"
	CatTest   ToolCategory = "test"
	CatMCP    ToolCategory = "mcp"
	CatWeb    ToolCategory = "web"
	CatOther  ToolCategory = "other"
)

type Severity string

const (
	SevInfo   Severity = "info"
	SevLow    Severity = "low"
	SevMedium Severity = "medium"
	SevHigh   Severity = "high"
)

func (s Severity) Rank() int {
	switch s {
	case SevInfo:
		return 0
	case SevLow:
		return 1
	case SevMedium:
		return 2
	case SevHigh:
		return 3
	}
	return -1
}

var Severities = []Severity{SevHigh, SevMedium, SevLow, SevInfo}

type ChangeType string

const (
	ChangeBehavior   ChangeType = "behavior"
	ChangeRefactor   ChangeType = "refactor"
	ChangeTest       ChangeType = "test"
	ChangeConfig     ChangeType = "config"
	ChangeDependency ChangeType = "dependency"
	ChangeDocs       ChangeType = "docs"
	ChangeGenerated  ChangeType = "generated"
	ChangeUnknown    ChangeType = "unknown"
)

const (
	OutcomePass    = "pass"
	OutcomeFail    = "fail"
	OutcomeError   = "error"
	OutcomeUnknown = "unknown"
)

type Repo struct {
	ID        string
	Path      string
	RemoteURL string
	CreatedAt time.Time
}

type Session struct {
	ID              string
	RepoID          string
	Agent           string
	AgentVersion    string
	AgentSessionID  string
	Models          []string
	Fidelity        Fidelity
	BaseCommit      string
	BaseSnapshot    string
	FinalSnapshot   string
	StartedAt       time.Time
	EndedAt         time.Time
	Cwd             string
	Title           string
	ParentSessionID string
	NoGit           bool
	InputTokens     int64
	OutputTokens    int64
	Status          string
}

type Actor struct {
	ID            string
	SessionID     string
	Kind          string
	Name          string
	ParentActorID string
}

type Step struct {
	ID           string
	SessionID    string
	ActorID      string
	ParentStepID string
	Seq          int
	TsStart      time.Time
	TsEnd        time.Time
	Kind         StepKind
	Text         string
	Snapshot     string
	Summary      string
}

type ToolCall struct {
	StepID     string
	ToolUseID  string
	ToolName   string
	Category   ToolCategory
	Input      string
	Output     string
	ExitCode   *int
	OK         bool
	DurationMS int64
}

type Edit struct {
	ID               string
	StepID           string
	FilePath         string
	OldPath          string
	BeforeBlob       string
	AfterBlob        string
	Patch            string
	Added            int
	Removed          int
	RevertedByEditID string
}

type TestRun struct {
	StepID    string
	Command   string
	Framework string
	Passed    int
	Failed    int
	Skipped   int
	Outcome   string
}

type IntentItem struct {
	ID           string
	SessionID    string
	Source       string
	SourceStepID string
	Text         string
	Status       string
}

type Change struct {
	ID         string
	RepoID     string
	BaseRef    string
	TargetRef  string
	SessionIDs []string
	CreatedAt  time.Time
}

type Hunk struct {
	ID           string
	ChangeID     string
	FilePath     string
	OldStart     int
	OldLines     int
	NewStart     int
	NewLines     int
	Patch        string
	ChangeType   ChangeType
	ReviewUnitID string
	ContentHash  string
}

type HunkProvenance struct {
	HunkID string
	StepID string
	Lines  int
}

type HunkIntent struct {
	HunkID       string
	IntentItemID string
	Confidence   float64
}

type Rationale struct {
	HunkID          string
	AgentClaim      string
	ClaimStepID     string
	Explanation     string
	EvidenceStepIDs []string
	Verified        bool
	Model           string
	CreatedAt       time.Time
}

type ReviewUnit struct {
	ID       string
	ChangeID string
	Title    string
	Ord      int
	Status   string
	Source   string
}

type Flag struct {
	ID              string
	ChangeID        string
	SessionID       string
	RuleID          string
	Severity        Severity
	Detector        string
	TargetKind      string
	TargetID        string
	Message         string
	EvidenceStepIDs []string
	Suppressed      bool
	SuppressReason  string
	Fingerprint     string
}

type HookEvent struct {
	SessionID string
	Key       string
	Seq       int
	Ts        time.Time
	Event     string
	Payload   string
	Snapshot  string
}

const Unattributed = "unattributed"

type QA struct {
	ID           string
	ChangeID     string
	Question     string
	Answer       string
	CitedStepIDs []string
	CitedHunkIDs []string
	Model        string
	CreatedAt    time.Time
}

type Blob struct {
	SHA256  string
	Size    int64
	Content []byte
}

func NewID() string { return ulid.Make().String() }

// StableID is deterministic so re-ingesting a session keeps the IDs that LLM output and review state point to.
func StableID(t time.Time, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(p)))
		h.Write(n[:])
		h.Write([]byte(p))
	}
	sum := h.Sum(nil)
	var id ulid.ULID
	if t.IsZero() || t.Before(time.Unix(0, 0)) {
		t = time.Unix(0, 0)
	}
	_ = id.SetTime(ulid.Timestamp(t))
	_ = id.SetEntropy(sum[:10])
	return id.String()
}

type SnapshotMark struct {
	Commit string
	StepID string
	Base   bool
}

// Trace is the output of an agent adapter: one session in canonical form,
// before redaction and before edits are derived from the snapshots.
type Trace struct {
	Session   Session
	Actors    []Actor
	Steps     []Step
	ToolCalls []ToolCall
	Snapshots []SnapshotMark
	Edits     []Edit
	Warnings  []string
}

func (t *Trace) ToolCall(stepID string) *ToolCall {
	for i := range t.ToolCalls {
		if t.ToolCalls[i].StepID == stepID {
			return &t.ToolCalls[i]
		}
	}
	return nil
}

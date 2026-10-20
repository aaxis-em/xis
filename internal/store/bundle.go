package store

import "github.com/aaxis-em/xis/internal/model"

type SessionData struct {
	Session   model.Session
	Steps     []model.Step
	Actors    map[string]model.Actor
	ToolCalls map[string]model.ToolCall
	Edits     []model.Edit
	TestRuns  []model.TestRun
	Intent    []model.IntentItem
	StepIndex map[string]int
}

func (s *Store) LoadSession(id string) (*SessionData, error) {
	sess, err := s.GetSession(id)
	if err != nil {
		return nil, err
	}
	d := &SessionData{Session: sess, Actors: map[string]model.Actor{}, ToolCalls: map[string]model.ToolCall{}, StepIndex: map[string]int{}}
	if d.Steps, err = s.ListSteps(id); err != nil {
		return nil, err
	}
	for i, st := range d.Steps {
		d.StepIndex[st.ID] = i
	}
	actors, err := s.ListActors(id)
	if err != nil {
		return nil, err
	}
	for _, a := range actors {
		d.Actors[a.ID] = a
	}
	tcs, err := s.ListToolCalls(id)
	if err != nil {
		return nil, err
	}
	for _, tc := range tcs {
		d.ToolCalls[tc.StepID] = tc
	}
	if d.Edits, err = s.ListEdits(id); err != nil {
		return nil, err
	}
	if d.TestRuns, err = s.ListTestRuns(id); err != nil {
		return nil, err
	}
	if d.Intent, err = s.ListIntentItems(id); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *SessionData) Step(id string) (model.Step, bool) {
	i, ok := d.StepIndex[id]
	if !ok {
		return model.Step{}, false
	}
	return d.Steps[i], true
}

func (d *SessionData) ActorKind(stepID string) string {
	st, ok := d.Step(stepID)
	if !ok {
		return ""
	}
	return d.Actors[st.ActorID].Kind
}

func (d *SessionData) EditsOf(stepID string) []model.Edit {
	var out []model.Edit
	for _, e := range d.Edits {
		if e.StepID == stepID {
			out = append(out, e)
		}
	}
	return out
}

func (d *SessionData) TestRunOf(stepID string) (model.TestRun, bool) {
	for _, r := range d.TestRuns {
		if r.StepID == stepID {
			return r, true
		}
	}
	return model.TestRun{}, false
}

// InstructionFor returns the last human prompt before the step.
func (d *SessionData) InstructionFor(stepID string) (model.Step, bool) {
	i, ok := d.StepIndex[stepID]
	if !ok {
		return model.Step{}, false
	}
	for j := i; j >= 0; j-- {
		if d.Steps[j].Kind == model.StepUserMessage {
			return d.Steps[j], true
		}
	}
	return model.Step{}, false
}

// AgentTextNear returns assistant and reasoning steps of the same actor among
// the n steps before stepID.
func (d *SessionData) AgentTextNear(stepID string, n int) []model.Step {
	i, ok := d.StepIndex[stepID]
	if !ok {
		return nil
	}
	actor := d.Steps[i].ActorID
	var out []model.Step
	for j := i - 1; j >= 0 && j >= i-n; j-- {
		st := d.Steps[j]
		if st.Kind == model.StepUserMessage {
			break
		}
		if st.ActorID == actor && (st.Kind == model.StepAssistantMessage || st.Kind == model.StepReasoning || st.Kind == model.StepPlan) {
			out = append([]model.Step{st}, out...)
		}
	}
	return out
}

// EvidenceBefore returns read, search and test tool calls before stepID.
func (d *SessionData) EvidenceBefore(stepID string, limit int) []model.Step {
	i, ok := d.StepIndex[stepID]
	if !ok {
		return nil
	}
	var out []model.Step
	for j := i - 1; j >= 0 && len(out) < limit; j-- {
		tc, ok := d.ToolCalls[d.Steps[j].ID]
		if ok && (tc.Category == model.CatRead || tc.Category == model.CatSearch || tc.Category == model.CatTest) {
			out = append([]model.Step{d.Steps[j]}, out...)
		}
	}
	return out
}

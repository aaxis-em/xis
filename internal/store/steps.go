package store

import (
	"database/sql"

	"github.com/aaxis-em/xis/internal/model"
)

const stepCols = `id, session_id, actor_id, parent_step_id, seq, ts_start, ts_end, kind, text, snapshot, summary`

func (s *Store) PutStep(x model.Step) error {
	return s.exec(`INSERT OR REPLACE INTO steps (`+stepCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.SessionID, nullStr(x.ActorID), nullStr(x.ParentStepID), x.Seq, ts(x.TsStart), ts(x.TsEnd),
		string(x.Kind), x.Text, nullStr(x.Snapshot), nullStr(x.Summary))
}

func scanStep(sc interface{ Scan(...any) error }) (model.Step, error) {
	var x model.Step
	var actor, parent, start, end, kind, text, snap, summary sql.NullString
	err := sc.Scan(&x.ID, &x.SessionID, &actor, &parent, &x.Seq, &start, &end, &kind, &text, &snap, &summary)
	x.ActorID, x.ParentStepID = str(actor), str(parent)
	x.TsStart, x.TsEnd = parseTS(start), parseTS(end)
	x.Kind, x.Text, x.Snapshot, x.Summary = model.StepKind(str(kind)), str(text), str(snap), str(summary)
	return x, err
}

func (s *Store) ListSteps(sessionID string) ([]model.Step, error) {
	rows, err := s.query(`SELECT `+stepCols+` FROM steps WHERE session_id = ? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Step
	for rows.Next() {
		x, err := scanStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) SetStepSummary(id, summary string) error {
	return s.exec(`UPDATE steps SET summary = ? WHERE id = ?`, summary, id)
}

const toolCols = `step_id, tool_use_id, tool_name, category, input, output, exit_code, ok, duration_ms`

func (s *Store) PutToolCall(x model.ToolCall) error {
	var exit any
	if x.ExitCode != nil {
		exit = *x.ExitCode
	}
	return s.exec(`INSERT OR REPLACE INTO tool_calls (`+toolCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		x.StepID, x.ToolUseID, x.ToolName, string(x.Category), x.Input, x.Output, exit, boolInt(x.OK), x.DurationMS)
}

func scanToolCall(sc interface{ Scan(...any) error }) (model.ToolCall, error) {
	var x model.ToolCall
	var tuid, name, cat, in, out sql.NullString
	var exit, ok, dur sql.NullInt64
	err := sc.Scan(&x.StepID, &tuid, &name, &cat, &in, &out, &exit, &ok, &dur)
	x.ToolUseID, x.ToolName, x.Category, x.Input, x.Output = str(tuid), str(name), model.ToolCategory(str(cat)), str(in), str(out)
	if exit.Valid {
		v := int(exit.Int64)
		x.ExitCode = &v
	}
	x.OK, x.DurationMS = ok.Int64 != 0, dur.Int64
	return x, err
}

func (s *Store) ListToolCalls(sessionID string) ([]model.ToolCall, error) {
	rows, err := s.query(`SELECT t.step_id, t.tool_use_id, t.tool_name, t.category, t.input, t.output, t.exit_code, t.ok, t.duration_ms
		FROM tool_calls t JOIN steps s ON s.id = t.step_id WHERE s.session_id = ? ORDER BY s.seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ToolCall
	for rows.Next() {
		x, err := scanToolCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

const editCols = `id, step_id, file_path, old_path, before_blob, after_blob, patch, added, removed, reverted_by_edit_id`

func (s *Store) PutEdit(x model.Edit) error {
	return s.exec(`INSERT OR REPLACE INTO edits (`+editCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.StepID, x.FilePath, nullStr(x.OldPath), nullStr(x.BeforeBlob), nullStr(x.AfterBlob), x.Patch,
		x.Added, x.Removed, nullStr(x.RevertedByEditID))
}

func scanEdit(sc interface{ Scan(...any) error }) (model.Edit, error) {
	var x model.Edit
	var old, before, after, patch, rev sql.NullString
	err := sc.Scan(&x.ID, &x.StepID, &x.FilePath, &old, &before, &after, &patch, &x.Added, &x.Removed, &rev)
	x.OldPath, x.BeforeBlob, x.AfterBlob, x.Patch, x.RevertedByEditID = str(old), str(before), str(after), str(patch), str(rev)
	return x, err
}

func (s *Store) ListEdits(sessionID string) ([]model.Edit, error) {
	rows, err := s.query(`SELECT e.id, e.step_id, e.file_path, e.old_path, e.before_blob, e.after_blob, e.patch, e.added, e.removed, e.reverted_by_edit_id
		FROM edits e JOIN steps s ON s.id = e.step_id WHERE s.session_id = ? ORDER BY s.seq, e.file_path`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Edit
	for rows.Next() {
		x, err := scanEdit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) PutTestRun(x model.TestRun) error {
	return s.exec(`INSERT OR REPLACE INTO test_runs (step_id, command, framework, passed, failed, skipped, outcome) VALUES (?,?,?,?,?,?,?)`,
		x.StepID, x.Command, x.Framework, x.Passed, x.Failed, x.Skipped, x.Outcome)
}

func (s *Store) ListTestRuns(sessionID string) ([]model.TestRun, error) {
	rows, err := s.query(`SELECT t.step_id, t.command, t.framework, t.passed, t.failed, t.skipped, t.outcome
		FROM test_runs t JOIN steps s ON s.id = t.step_id WHERE s.session_id = ? ORDER BY s.seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TestRun
	for rows.Next() {
		var x model.TestRun
		var cmd, fw, outc sql.NullString
		var p, f, sk sql.NullInt64
		if err := rows.Scan(&x.StepID, &cmd, &fw, &p, &f, &sk, &outc); err != nil {
			return nil, err
		}
		x.Command, x.Framework, x.Outcome = str(cmd), str(fw), str(outc)
		x.Passed, x.Failed, x.Skipped = int(p.Int64), int(f.Int64), int(sk.Int64)
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) PutIntentItem(x model.IntentItem) error {
	return s.exec(`INSERT OR REPLACE INTO intent_items (id, session_id, source, source_step_id, text, status) VALUES (?,?,?,?,?,?)`,
		x.ID, x.SessionID, x.Source, nullStr(x.SourceStepID), x.Text, x.Status)
}

func (s *Store) DeleteIntentItems(sessionID string) error {
	return s.exec(`DELETE FROM intent_items WHERE session_id = ?`, sessionID)
}

func (s *Store) ListIntentItems(sessionIDs ...string) ([]model.IntentItem, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	rows, err := s.query(`SELECT id, session_id, source, source_step_id, text, status FROM intent_items WHERE session_id IN (`+placeholders(len(sessionIDs))+`) ORDER BY id`, anyList(sessionIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.IntentItem
	for rows.Next() {
		var x model.IntentItem
		var src, step, text, status sql.NullString
		if err := rows.Scan(&x.ID, &x.SessionID, &src, &step, &text, &status); err != nil {
			return nil, err
		}
		x.Source, x.SourceStepID, x.Text, x.Status = str(src), str(step), str(text), str(status)
		out = append(out, x)
	}
	return out, rows.Err()
}

package store

import (
	"database/sql"
	"errors"

	"github.com/aaxis-em/xis/internal/model"
)

func (s *Store) PutChange(c model.Change) error {
	return s.exec(`INSERT OR REPLACE INTO changes (id, repo_id, base_ref, target_ref, session_ids, created_at) VALUES (?,?,?,?,?,?)`,
		c.ID, c.RepoID, c.BaseRef, c.TargetRef, jsonList(c.SessionIDs), ts(c.CreatedAt))
}

func (s *Store) GetChange(id string) (model.Change, error) {
	var c model.Change
	var repo, base, target, sids, created sql.NullString
	err := s.row(`SELECT id, repo_id, base_ref, target_ref, session_ids, created_at FROM changes WHERE id = ?`, id).
		Scan(&c.ID, &repo, &base, &target, &sids, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.RepoID, c.BaseRef, c.TargetRef, c.SessionIDs, c.CreatedAt = str(repo), str(base), str(target), parseList(sids), parseTS(created)
	return c, err
}

func (s *Store) DeleteChangeHunks(changeID string) error {
	for _, q := range []string{
		`DELETE FROM hunk_provenance WHERE hunk_id IN (SELECT id FROM hunks WHERE change_id = ?)`,
		`DELETE FROM hunk_intent WHERE hunk_id IN (SELECT id FROM hunks WHERE change_id = ?)`,
		`DELETE FROM hunks WHERE change_id = ?`,
	} {
		if err := s.exec(q, changeID); err != nil {
			return err
		}
	}
	return nil
}

const hunkCols = `id, change_id, file_path, old_start, old_lines, new_start, new_lines, patch, change_type, review_unit_id, content_hash`

func (s *Store) PutHunk(h model.Hunk) error {
	return s.exec(`INSERT OR REPLACE INTO hunks (`+hunkCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		h.ID, h.ChangeID, h.FilePath, h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Patch, string(h.ChangeType),
		nullStr(h.ReviewUnitID), h.ContentHash)
}

func scanHunk(sc interface{ Scan(...any) error }) (model.Hunk, error) {
	var h model.Hunk
	var patch, ct, unit, hash sql.NullString
	err := sc.Scan(&h.ID, &h.ChangeID, &h.FilePath, &h.OldStart, &h.OldLines, &h.NewStart, &h.NewLines, &patch, &ct, &unit, &hash)
	h.Patch, h.ChangeType, h.ReviewUnitID, h.ContentHash = str(patch), model.ChangeType(str(ct)), str(unit), str(hash)
	return h, err
}

func (s *Store) GetHunk(id string) (model.Hunk, error) {
	h, err := scanHunk(s.row(`SELECT `+hunkCols+` FROM hunks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return h, ErrNotFound
	}
	return h, err
}

func (s *Store) ListHunks(changeID string) ([]model.Hunk, error) {
	rows, err := s.query(`SELECT `+hunkCols+` FROM hunks WHERE change_id = ? ORDER BY file_path, new_start, old_start`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Hunk
	for rows.Next() {
		h, err := scanHunk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) SetHunkUnit(hunkID, unitID string) error {
	return s.exec(`UPDATE hunks SET review_unit_id = ? WHERE id = ?`, unitID, hunkID)
}

func (s *Store) SetHunkChangeType(hunkID string, ct model.ChangeType) error {
	return s.exec(`UPDATE hunks SET change_type = ? WHERE id = ?`, string(ct), hunkID)
}

func (s *Store) PutProvenance(p model.HunkProvenance) error {
	return s.exec(`INSERT OR REPLACE INTO hunk_provenance (hunk_id, step_id, lines) VALUES (?,?,?)`, p.HunkID, p.StepID, p.Lines)
}

func (s *Store) ListProvenance(changeID string) ([]model.HunkProvenance, error) {
	rows, err := s.query(`SELECT p.hunk_id, p.step_id, p.lines FROM hunk_provenance p JOIN hunks h ON h.id = p.hunk_id
		WHERE h.change_id = ? ORDER BY p.hunk_id, p.step_id`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.HunkProvenance
	for rows.Next() {
		var p model.HunkProvenance
		if err := rows.Scan(&p.HunkID, &p.StepID, &p.Lines); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) HunksOfStep(changeID, stepID string) ([]string, error) {
	rows, err := s.query(`SELECT p.hunk_id FROM hunk_provenance p JOIN hunks h ON h.id = p.hunk_id WHERE h.change_id = ? AND p.step_id = ?`, changeID, stepID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) PutHunkIntent(x model.HunkIntent) error {
	return s.exec(`INSERT OR REPLACE INTO hunk_intent (hunk_id, intent_item_id, confidence) VALUES (?,?,?)`, x.HunkID, x.IntentItemID, x.Confidence)
}

func (s *Store) ListHunkIntent(changeID string) ([]model.HunkIntent, error) {
	rows, err := s.query(`SELECT i.hunk_id, i.intent_item_id, i.confidence FROM hunk_intent i JOIN hunks h ON h.id = i.hunk_id WHERE h.change_id = ?`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.HunkIntent
	for rows.Next() {
		var x model.HunkIntent
		if err := rows.Scan(&x.HunkID, &x.IntentItemID, &x.Confidence); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) PutRationale(r model.Rationale) error {
	return s.exec(`INSERT OR REPLACE INTO rationales (hunk_id, agent_claim, claim_step_id, explanation, evidence_step_ids, verified, model, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		r.HunkID, r.AgentClaim, nullStr(r.ClaimStepID), r.Explanation, jsonList(r.EvidenceStepIDs), boolInt(r.Verified), r.Model, ts(r.CreatedAt))
}

func scanRationale(sc interface{ Scan(...any) error }) (model.Rationale, error) {
	var r model.Rationale
	var claim, step, expl, ev, mdl, created sql.NullString
	var ver sql.NullInt64
	err := sc.Scan(&r.HunkID, &claim, &step, &expl, &ev, &ver, &mdl, &created)
	r.AgentClaim, r.ClaimStepID, r.Explanation, r.EvidenceStepIDs = str(claim), str(step), str(expl), parseList(ev)
	r.Verified, r.Model, r.CreatedAt = ver.Int64 != 0, str(mdl), parseTS(created)
	return r, err
}

func (s *Store) GetRationale(hunkID string) (model.Rationale, error) {
	r, err := scanRationale(s.row(`SELECT hunk_id, agent_claim, claim_step_id, explanation, evidence_step_ids, verified, model, created_at FROM rationales WHERE hunk_id = ?`, hunkID))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) ListRationales(changeID string) ([]model.Rationale, error) {
	rows, err := s.query(`SELECT r.hunk_id, r.agent_claim, r.claim_step_id, r.explanation, r.evidence_step_ids, r.verified, r.model, r.created_at
		FROM rationales r JOIN hunks h ON h.id = r.hunk_id WHERE h.change_id = ?`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Rationale
	for rows.Next() {
		r, err := scanRationale(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PutReviewUnit(u model.ReviewUnit) error {
	if u.Status == "" {
		u.Status = "unreviewed"
	}
	return s.exec(`INSERT INTO review_units (id, change_id, title, ord, status, source) VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, ord=excluded.ord, source=excluded.source`,
		u.ID, u.ChangeID, u.Title, u.Ord, u.Status, u.Source)
}

func (s *Store) DeleteReviewUnitsExcept(changeID string, keep []string) error {
	if len(keep) == 0 {
		return s.exec(`DELETE FROM review_units WHERE change_id = ?`, changeID)
	}
	args := append([]any{changeID}, anyList(keep)...)
	return s.exec(`DELETE FROM review_units WHERE change_id = ? AND id NOT IN (`+placeholders(len(keep))+`)`, args...)
}

func (s *Store) ListReviewUnits(changeID string) ([]model.ReviewUnit, error) {
	rows, err := s.query(`SELECT id, change_id, title, ord, status, source FROM review_units WHERE change_id = ? ORDER BY ord`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ReviewUnit
	for rows.Next() {
		var u model.ReviewUnit
		var title, status, source sql.NullString
		if err := rows.Scan(&u.ID, &u.ChangeID, &title, &u.Ord, &status, &source); err != nil {
			return nil, err
		}
		u.Title, u.Status, u.Source = str(title), str(status), str(source)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetReviewUnitStatus(id, status string) error {
	return s.exec(`UPDATE review_units SET status = ? WHERE id = ?`, status, id)
}

func (s *Store) PutQA(x model.QA) error {
	return s.exec(`INSERT OR REPLACE INTO qa (id, change_id, question, answer, cited_step_ids, cited_hunk_ids, model, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		x.ID, x.ChangeID, x.Question, x.Answer, jsonList(x.CitedStepIDs), jsonList(x.CitedHunkIDs), x.Model, ts(x.CreatedAt))
}

func (s *Store) ListQA(changeID string) ([]model.QA, error) {
	rows, err := s.query(`SELECT id, change_id, question, answer, cited_step_ids, cited_hunk_ids, model, created_at FROM qa WHERE change_id = ? ORDER BY created_at`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.QA
	for rows.Next() {
		var x model.QA
		var q, a, cs, ch, m, c sql.NullString
		if err := rows.Scan(&x.ID, &x.ChangeID, &q, &a, &cs, &ch, &m, &c); err != nil {
			return nil, err
		}
		x.Question, x.Answer, x.CitedStepIDs, x.CitedHunkIDs, x.Model, x.CreatedAt = str(q), str(a), parseList(cs), parseList(ch), str(m), parseTS(c)
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) ListChanges(repoID string) ([]model.Change, error) {
	rows, err := s.query(`SELECT id, repo_id, base_ref, target_ref, session_ids, created_at FROM changes WHERE repo_id = ? ORDER BY created_at DESC`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Change
	for rows.Next() {
		var c model.Change
		var repo, base, target, sids, created sql.NullString
		if err := rows.Scan(&c.ID, &repo, &base, &target, &sids, &created); err != nil {
			return nil, err
		}
		c.RepoID, c.BaseRef, c.TargetRef, c.SessionIDs, c.CreatedAt = str(repo), str(base), str(target), parseList(sids), parseTS(created)
		out = append(out, c)
	}
	return out, rows.Err()
}

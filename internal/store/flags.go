package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/aaxis-em/xis/internal/model"
)

const flagCols = `f.id, f.change_id, f.session_id, f.rule_id, f.severity, f.detector, f.target_kind, f.target_id,
	f.message, f.evidence_step_ids, f.suppressed, f.fingerprint, s.reason`

func (s *Store) PutFlag(f model.Flag) error {
	return s.exec(`INSERT OR REPLACE INTO flags (id, change_id, session_id, rule_id, severity, detector, target_kind, target_id, message, evidence_step_ids, suppressed, fingerprint)
		VALUES (?,?,?,?,?,?,?,?,?,?, CASE WHEN ? = 1 OR EXISTS (SELECT 1 FROM suppressions WHERE fingerprint = ?) THEN 1 ELSE 0 END, ?)`,
		f.ID, f.ChangeID, nullStr(f.SessionID), f.RuleID, string(f.Severity), f.Detector, f.TargetKind, f.TargetID,
		f.Message, jsonList(f.EvidenceStepIDs), boolInt(f.Suppressed), f.Fingerprint, f.Fingerprint)
}

func (s *Store) DeleteFlags(changeID, detector string) error {
	return s.exec(`DELETE FROM flags WHERE change_id = ? AND detector = ?`, changeID, detector)
}

func scanFlag(sc interface{ Scan(...any) error }) (model.Flag, error) {
	var f model.Flag
	var change, sess, rule, sev, det, tk, tid, msg, ev, fp, reason sql.NullString
	var sup sql.NullInt64
	err := sc.Scan(&f.ID, &change, &sess, &rule, &sev, &det, &tk, &tid, &msg, &ev, &sup, &fp, &reason)
	f.ChangeID, f.SessionID, f.RuleID, f.Severity, f.Detector = str(change), str(sess), str(rule), model.Severity(str(sev)), str(det)
	f.TargetKind, f.TargetID, f.Message, f.EvidenceStepIDs = str(tk), str(tid), str(msg), parseList(ev)
	f.Suppressed, f.Fingerprint, f.SuppressReason = sup.Int64 != 0, str(fp), str(reason)
	return f, err
}

func (s *Store) GetFlag(id string) (model.Flag, error) {
	f, err := scanFlag(s.row(`SELECT `+flagCols+` FROM flags f LEFT JOIN suppressions s ON s.fingerprint = f.fingerprint WHERE f.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

func (s *Store) ListFlags(changeID string) ([]model.Flag, error) {
	rows, err := s.query(`SELECT `+flagCols+` FROM flags f LEFT JOIN suppressions s ON s.fingerprint = f.fingerprint WHERE f.change_id = ?
		ORDER BY CASE f.severity WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 ELSE 3 END, f.rule_id, f.target_id`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Flag
	for rows.Next() {
		f, err := scanFlag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) SuppressFlag(id, reason string) (model.Flag, error) {
	f, err := s.GetFlag(id)
	if err != nil {
		return f, err
	}
	err = s.Tx(func(tx *Store) error {
		if err := tx.exec(`INSERT OR REPLACE INTO suppressions (fingerprint, flag_id, rule_id, reason, created_at) VALUES (?,?,?,?,?)`,
			f.Fingerprint, f.ID, f.RuleID, reason, ts(time.Now())); err != nil {
			return err
		}
		return tx.exec(`UPDATE flags SET suppressed = 1 WHERE fingerprint = ? OR id = ?`, f.Fingerprint, f.ID)
	})
	if err != nil {
		return f, err
	}
	return s.GetFlag(id)
}

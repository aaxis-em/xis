package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/aaxis-em/xis/internal/model"
)

func (s *Store) PutRepo(r model.Repo) error {
	return s.exec(`INSERT INTO repos (id, path, remote_url, created_at) VALUES (?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET path=excluded.path, remote_url=COALESCE(NULLIF(excluded.remote_url, ''), repos.remote_url)`,
		r.ID, r.Path, r.RemoteURL, ts(r.CreatedAt))
}

func (s *Store) GetRepo(id string) (model.Repo, error) {
	var r model.Repo
	var path, remote, created sql.NullString
	err := s.row(`SELECT id, path, remote_url, created_at FROM repos WHERE id = ?`, id).Scan(&r.ID, &path, &remote, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	r.Path, r.RemoteURL, r.CreatedAt = str(path), str(remote), parseTS(created)
	return r, err
}

const sessionCols = `id, repo_id, agent, agent_version, agent_session_id, models, fidelity,
	base_commit, base_snapshot, final_snapshot, started_at, ended_at, cwd, title,
	parent_session_id, no_git, input_tokens, output_tokens, status`

func (s *Store) PutSession(x model.Session) error {
	return s.exec(`INSERT OR REPLACE INTO sessions (`+sessionCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.RepoID, x.Agent, x.AgentVersion, x.AgentSessionID, jsonList(x.Models), string(x.Fidelity),
		nullStr(x.BaseCommit), nullStr(x.BaseSnapshot), nullStr(x.FinalSnapshot), ts(x.StartedAt), ts(x.EndedAt),
		x.Cwd, x.Title, nullStr(x.ParentSessionID), boolInt(x.NoGit), x.InputTokens, x.OutputTokens, x.Status)
}

func scanSession(sc interface{ Scan(...any) error }) (model.Session, error) {
	var x model.Session
	var repo, agent, ver, asid, models, fid, base, bsnap, fsnap, started, ended, cwd, title, parent, status sql.NullString
	var noGit sql.NullInt64
	var in, out sql.NullInt64
	err := sc.Scan(&x.ID, &repo, &agent, &ver, &asid, &models, &fid, &base, &bsnap, &fsnap, &started, &ended,
		&cwd, &title, &parent, &noGit, &in, &out, &status)
	if err != nil {
		return x, err
	}
	x.RepoID, x.Agent, x.AgentVersion, x.AgentSessionID = str(repo), str(agent), str(ver), str(asid)
	x.Models = parseList(models)
	x.Fidelity = model.Fidelity(str(fid))
	x.BaseCommit, x.BaseSnapshot, x.FinalSnapshot = str(base), str(bsnap), str(fsnap)
	x.StartedAt, x.EndedAt = parseTS(started), parseTS(ended)
	x.Cwd, x.Title, x.ParentSessionID, x.Status = str(cwd), str(title), str(parent), str(status)
	x.NoGit = noGit.Int64 != 0
	x.InputTokens, x.OutputTokens = in.Int64, out.Int64
	return x, nil
}

func (s *Store) GetSession(id string) (model.Session, error) {
	x, err := scanSession(s.row(`SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

func (s *Store) FindSession(agent, agentSessionID string) (model.Session, error) {
	x, err := scanSession(s.row(`SELECT `+sessionCols+` FROM sessions WHERE agent = ? AND agent_session_id = ? AND parent_session_id IS NULL`, agent, agentSessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

func (s *Store) ListSessions(repoID string, limit int) ([]model.Session, error) {
	q := `SELECT ` + sessionCols + ` FROM sessions WHERE (? = '' OR repo_id = ?) ORDER BY started_at DESC, id DESC`
	args := []any{repoID, repoID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) SessionsBefore(t time.Time) ([]model.Session, error) {
	rows, err := s.query(`SELECT `+sessionCols+` FROM sessions WHERE started_at < ?`, ts(t))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSessionData(sessionID string) error {
	for _, q := range []string{
		`DELETE FROM tool_calls WHERE step_id IN (SELECT id FROM steps WHERE session_id = ?)`,
		`DELETE FROM edits WHERE step_id IN (SELECT id FROM steps WHERE session_id = ?)`,
		`DELETE FROM test_runs WHERE step_id IN (SELECT id FROM steps WHERE session_id = ?)`,
		`DELETE FROM steps WHERE session_id = ?`,
		`DELETE FROM actors WHERE session_id = ?`,
	} {
		if err := s.exec(q, sessionID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) PutActor(a model.Actor) error {
	return s.exec(`INSERT OR REPLACE INTO actors (id, session_id, kind, name, parent_actor_id) VALUES (?,?,?,?,?)`,
		a.ID, a.SessionID, a.Kind, a.Name, nullStr(a.ParentActorID))
}

func (s *Store) ListActors(sessionID string) ([]model.Actor, error) {
	rows, err := s.query(`SELECT id, session_id, kind, name, parent_actor_id FROM actors WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Actor
	for rows.Next() {
		var a model.Actor
		var kind, name, parent sql.NullString
		if err := rows.Scan(&a.ID, &a.SessionID, &kind, &name, &parent); err != nil {
			return nil, err
		}
		a.Kind, a.Name, a.ParentActorID = str(kind), str(name), str(parent)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) PutHookEvent(e model.HookEvent) error {
	return s.exec(`INSERT OR IGNORE INTO hook_events (session_id, key, seq, ts, event, payload, snapshot) VALUES (?,?,?,?,?,?,?)`,
		e.SessionID, e.Key, e.Seq, ts(e.Ts), e.Event, e.Payload, nullStr(e.Snapshot))
}

func (s *Store) ListHookEvents(sessionID string) ([]model.HookEvent, error) {
	rows, err := s.query(`SELECT session_id, key, seq, ts, event, payload, snapshot FROM hook_events WHERE session_id = ? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.HookEvent
	for rows.Next() {
		var e model.HookEvent
		var t, ev, payload, snap sql.NullString
		if err := rows.Scan(&e.SessionID, &e.Key, &e.Seq, &t, &ev, &payload, &snap); err != nil {
			return nil, err
		}
		e.Ts, e.Event, e.Payload, e.Snapshot = parseTS(t), str(ev), str(payload), str(snap)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) HasPrompt(sessionID string) (bool, error) {
	var n int
	err := s.row(`SELECT COUNT(*) FROM steps WHERE session_id = ? AND kind = 'user_message'`, sessionID).Scan(&n)
	return n > 0, err
}

package capture

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/lockfile"
	"github.com/aaxis-em/xis/internal/snapshot"
)

const SpoolVersion = 1

type RepoInfo struct {
	ID        string `json:"id"`
	Top       string `json:"top"`
	CommonDir string `json:"common_dir,omitempty"`
	Head      string `json:"head,omitempty"`
	Remote    string `json:"remote,omitempty"`
}

type Line struct {
	V             int             `json:"xis_v"`
	Ts            time.Time       `json:"ts"`
	Event         string          `json:"event"`
	Payload       json.RawMessage `json:"payload"`
	Snapshot      *string         `json:"snapshot"`
	Repo          *RepoInfo       `json:"repo,omitempty"`
	NoGit         bool            `json:"no_git,omitempty"`
	SnapshotError string          `json:"snapshot_error,omitempty"`
}

func (l Line) SnapshotID() string {
	if l.Snapshot == nil {
		return ""
	}
	return *l.Snapshot
}

func SpoolPath(spoolDir, sessionID string) string {
	return filepath.Join(spoolDir, snapshot.SafeName(sessionID)+".jsonl")
}

func sessionLockPath(spoolDir, sessionID string) string {
	return filepath.Join(spoolDir, snapshot.SafeName(sessionID)+".lock")
}

func AppendLine(spoolDir, sessionID string, l Line) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(spoolDir, 0o700); err != nil {
		return err
	}
	path := SpoolPath(spoolDir, sessionID)
	lock, err := lockfile.Acquire(path+".wlock", 5*time.Second)
	if err != nil {
		return err
	}
	defer lock.Release()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func ReadSpool(path string) ([]Line, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Line
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	n := 0
	for sc.Scan() {
		n++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var l Line
		if err := json.Unmarshal(raw, &l); err != nil {
			return out, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out = append(out, l)
	}
	return out, sc.Err()
}

type SpoolFile struct {
	SessionID string
	Path      string
	ModTime   time.Time
	Size      int64
}

func ListSpool(spoolDir string) ([]SpoolFile, error) {
	entries, err := os.ReadDir(spoolDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SpoolFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, SpoolFile{
			SessionID: strings.TrimSuffix(name, ".jsonl"),
			Path:      filepath.Join(spoolDir, name),
			ModTime:   info.ModTime(),
			Size:      info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.Before(out[j].ModTime) })
	return out, nil
}

func RemoveSpool(spoolDir, sessionID string) error {
	path := SpoolPath(spoolDir, sessionID)
	err := os.Remove(path)
	_ = os.Remove(path + ".wlock")
	_ = os.Remove(sessionLockPath(spoolDir, sessionID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// LockSession serializes ingest with the hooks of the same session.
func LockSession(spoolDir, sessionID string, timeout time.Duration) (*lockfile.Lock, error) {
	return lockfile.Acquire(sessionLockPath(spoolDir, sessionID), timeout)
}

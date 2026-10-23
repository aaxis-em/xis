package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/lockfile"
)

type Repo struct {
	Top        string
	GitDir     string
	CommonDir  string
	ObjectsDir string
	ID         string
}

var ErrNotGit = errors.New("not inside a git repository")

func Discover(dir string) (Repo, error) {
	out, err := gitx.In(dir).Run("rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir")
	if err != nil {
		if gitx.ExitCode(err) == 128 {
			return Repo{}, ErrNotGit
		}
		return Repo{}, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 3 || lines[0] == "" {
		return Repo{}, ErrNotGit
	}
	r := Repo{Top: lines[0], GitDir: lines[1], CommonDir: filepath.Clean(lines[2])}
	r.ObjectsDir = filepath.Join(r.CommonDir, "objects")
	r.ID = RepoID(r.CommonDir)
	return r, nil
}

func RepoID(commonDir string) string {
	abs, err := filepath.Abs(commonDir)
	if err != nil {
		abs = commonDir
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
}

func (r Repo) Git() gitx.Git { return gitx.In(r.Top) }

func (r Repo) Head() string {
	out, err := r.Git().Run("rev-parse", "--verify", "-q", "HEAD^{commit}")
	if err != nil {
		return ""
	}
	return out
}

func (r Repo) RemoteURL() string {
	out, _ := r.Git().Run("config", "--get", "remote.origin.url")
	return out
}

type Shadow struct {
	Path    string
	Repo    Repo
	Exclude []string
}

func Open(shadowRoot string, r Repo, exclude []string) (*Shadow, error) {
	s := &Shadow{Path: filepath.Join(shadowRoot, r.ID+".git"), Repo: r, Exclude: exclude}
	if _, err := os.Stat(filepath.Join(s.Path, "HEAD")); err != nil {
		if err := os.MkdirAll(shadowRoot, 0o700); err != nil {
			return nil, err
		}
		lock, err := lockfile.Acquire(s.Path+".init.lock", 10*time.Second)
		if err != nil {
			return nil, err
		}
		defer lock.Release()
		if _, err := os.Stat(filepath.Join(s.Path, "HEAD")); err != nil {
			if _, err := gitx.In(shadowRoot).Run("init", "--quiet", "--bare", s.Path); err != nil {
				return nil, err
			}
			_ = os.Chmod(s.Path, 0o700)
			if _, err := s.Git().Run("config", "gc.auto", "0"); err != nil {
				return nil, err
			}
		}
	}
	if err := s.writeAlternates(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Shadow) writeAlternates() error {
	path := filepath.Join(s.Path, "objects", "info", "alternates")
	want := s.Repo.ObjectsDir + "\n"
	if b, err := os.ReadFile(path); err == nil && string(b) == want {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(want), 0o600)
}

func (s *Shadow) Git() gitx.Git {
	return gitx.In(s.Path).With("GIT_DIR=" + s.Path)
}

func SessionRef(sessionID string) string { return "refs/xis/sessions/" + SafeName(sessionID) }

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func SafeName(s string) string {
	s = unsafeChars.ReplaceAllString(s, "_")
	if s == "" || strings.HasPrefix(s, ".") {
		s = "_" + s
	}
	return s
}

func (s *Shadow) indexPath(name string) string {
	return filepath.Join(s.Path, "index-"+SafeName(name))
}

func (s *Shadow) worktreeEnv(index string) []string {
	return []string{
		"GIT_INDEX_FILE=" + index,
		"GIT_OBJECT_DIRECTORY=" + filepath.Join(s.Path, "objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + s.Repo.ObjectsDir,
	}
}

func (s *Shadow) excludePathspecs() []string {
	var out []string
	for _, p := range s.Exclude {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		switch {
		case strings.HasSuffix(p, "/"):
			p = "**/" + strings.TrimSuffix(p, "/") + "/**"
		case !strings.Contains(p, "/"):
			p = "**/" + p
		}
		out = append(out, ":(top,exclude,glob)"+p)
	}
	return out
}

// seedIndex copies the user's index so git add -A can reuse its stat cache
// instead of hashing every file on the first snapshot.
func (s *Shadow) seedIndex(dst string) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	if err := copyIndex(filepath.Join(s.Repo.GitDir, "index"), dst); err != nil {
		_ = os.Remove(dst)
	}
}

// copyIndex keeps the modification time of the source: git treats entries
// as racily clean by comparing them to the index file's mtime, and a newer
// mtime would hide same-size edits made in the same second.
func copyIndex(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, fi.ModTime(), fi.ModTime())
}

// WriteTree writes the tree of the working tree into the shadow object store. The
// user's index, refs and objects stay unchanged.
func (s *Shadow) WriteTree(indexName string) (string, error) {
	index := s.indexPath(indexName)
	s.seedIndex(index)
	g := s.Repo.Git().With(s.worktreeEnv(index)...)
	args := append([]string{"add", "-A", "--ignore-errors", "--", "."}, s.excludePathspecs()...)
	if _, err := g.Run(args...); err != nil && gitx.ExitCode(err) != 1 {
		return "", err
	}
	return g.Run("write-tree")
}

func (s *Shadow) Head(sessionID string) string {
	out, err := s.Git().Run("rev-parse", "--verify", "-q", SessionRef(sessionID))
	if err != nil {
		return ""
	}
	return out
}

func (s *Shadow) CommitTree(tree, message string, parents ...string) (string, error) {
	args := []string{"commit-tree", tree, "-m", message}
	for _, p := range parents {
		if p != "" {
			args = append(args, "-p", p)
		}
	}
	return s.Git().With(
		"GIT_AUTHOR_NAME=xis", "GIT_AUTHOR_EMAIL=xis@localhost",
		"GIT_COMMITTER_NAME=xis", "GIT_COMMITTER_EMAIL=xis@localhost",
	).Run(args...)
}

// Take records the working tree as a new commit on the session's chain. The
// first snapshot of a session has the user's HEAD as its parent.
func (s *Shadow) Take(sessionID, label string) (string, error) {
	lock, err := lockfile.Acquire(filepath.Join(s.Path, "locks", SafeName(sessionID)+".lock"), 20*time.Second)
	if err != nil {
		return "", fmt.Errorf("snapshot lock: %w", err)
	}
	defer lock.Release()
	tree, err := s.WriteTree(sessionID)
	if err != nil {
		return "", err
	}
	parent := s.Head(sessionID)
	if parent == "" {
		parent = s.Repo.Head()
	}
	commit, err := s.CommitTree(tree, "xis step "+label, parent)
	if err != nil {
		return "", err
	}
	if _, err := s.Git().Run("update-ref", SessionRef(sessionID), commit); err != nil {
		return "", err
	}
	return commit, nil
}

func (s *Shadow) TreeOf(commit string) (string, error) {
	return s.Git().Run("rev-parse", commit+"^{tree}")
}

func (s *Shadow) DeleteSession(sessionID string) error {
	_, err := s.Git().Run("update-ref", "-d", SessionRef(sessionID))
	_ = os.Remove(s.indexPath(sessionID))
	return err
}

func (s *Shadow) GC() error {
	_, err := s.Git().Run("gc", "--quiet", "--prune=now")
	return err
}

// WriteIndexTree writes the tree of a copy of a user index, so git can not
// touch the user's index file.
func (s *Shadow) WriteIndexTree(userIndex string) (string, error) {
	tmp := s.indexPath("staged-copy")
	if err := copyIndex(userIndex, tmp); err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	return s.Repo.Git().With(s.worktreeEnv(tmp)...).Run("write-tree")
}

func (s *Shadow) EmptyTree() (string, error) {
	return s.Git().WithStdin(strings.NewReader("")).Run("mktree")
}

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DirMode and FileMode are the permissions for everything xis creates.
const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

// Paths holds the absolute locations of the xis config and data files.
type Paths struct {
	ConfigDir  string
	ConfigFile string
	DataDir    string
	DB         string
	Spool      string
	Shadow     string
	Keys       string
	Logs       string
	LogFile    string
	LLMCache   string
}

// DefaultPaths returns the XDG paths. xis uses XDG paths on Linux and macOS.
func DefaultPaths() Paths {
	home, _ := os.UserHomeDir()
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" || !filepath.IsAbs(cfg) {
		cfg = filepath.Join(home, ".config")
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" || !filepath.IsAbs(data) {
		data = filepath.Join(home, ".local", "share")
	}
	return PathsAt(filepath.Join(cfg, "xis"), filepath.Join(data, "xis"))
}

// PathsAt returns the paths for a given config directory and data directory.
func PathsAt(configDir, dataDir string) Paths {
	return Paths{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "config.toml"),
		DataDir:    dataDir,
		DB:         filepath.Join(dataDir, "xis.db"),
		Spool:      filepath.Join(dataDir, "spool"),
		Shadow:     filepath.Join(dataDir, "shadow"),
		Keys:       filepath.Join(dataDir, "keys"),
		Logs:       filepath.Join(dataDir, "logs"),
		LogFile:    filepath.Join(dataDir, "logs", "xis.log"),
		LLMCache:   filepath.Join(dataDir, "cache", "llm"),
	}
}

// Ensure creates all directories with mode 0700.
func (p Paths) Ensure() error {
	for _, d := range []string{p.ConfigDir, p.DataDir, p.Spool, p.Shadow, p.Keys, p.Logs, filepath.Dir(p.LLMCache), p.LLMCache} {
		if err := EnsureDir(d); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDir creates dir and its parents with mode 0700. It also fixes the
// mode of dir if it exists with wider permissions.
func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	return os.Chmod(dir, DirMode)
}

// WriteFile writes data to path with mode 0600. It creates the parent
// directory with mode 0700 if necessary.
func WriteFile(path string, data []byte) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, FileMode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, FileMode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

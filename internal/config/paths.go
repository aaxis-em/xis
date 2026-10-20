package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

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

func (p Paths) Ensure() error {
	for _, d := range []string{p.ConfigDir, p.DataDir, p.Spool, p.Shadow, p.Keys, p.Logs, filepath.Dir(p.LLMCache), p.LLMCache} {
		if err := EnsureDir(d); err != nil {
			return err
		}
	}
	return nil
}

func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	return os.Chmod(dir, DirMode)
}

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

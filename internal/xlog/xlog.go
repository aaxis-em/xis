package xlog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	maxSize  = 5 << 20
	maxFiles = 5
)

type Writer struct {
	mu   sync.Mutex
	path string
}

func NewWriter(path string) *Writer { return &Writer{path: path} }

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return 0, err
	}
	if fi, err := os.Stat(w.path); err == nil && fi.Size()+int64(len(p)) > maxSize {
		w.rotate()
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Write(p)
}

func (w *Writer) rotate() {
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, maxFiles-1))
	for i := maxFiles - 2; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1))
	}
	_ = os.Rename(w.path, w.path+".1")
}

func Init(path string, verbose bool) {
	level := slog.LevelInfo
	var out io.Writer = NewWriter(path)
	if verbose {
		level = slog.LevelDebug
		out = io.MultiWriter(out, os.Stderr)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})))
}

func Discard() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

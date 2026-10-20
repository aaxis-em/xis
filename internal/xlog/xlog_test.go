package xlog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "xis.log")
	w := NewWriter(path)
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 30; i++ {
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i < maxFiles; i++ {
		if _, err := os.Stat(fmt.Sprintf("%s.%d", path, i)); err != nil {
			t.Errorf("missing rotated file %d", i)
		}
	}
	if _, err := os.Stat(fmt.Sprintf("%s.%d", path, maxFiles)); err == nil {
		t.Errorf("too many files kept")
	}
	fi, _ := os.Stat(path)
	if fi.Size() > maxSize {
		t.Errorf("size %d > max", fi.Size())
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}

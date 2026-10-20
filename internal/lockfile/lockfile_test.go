package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	a, err := Acquire(p, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(p, 20*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
	a.Release()
	b, err := Acquire(p, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b.Release()
}

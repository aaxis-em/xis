//go:build !unix && !windows

package lockfile

import "os"

// Capture is not supported on this platform; the lock only keeps the code compiling.
func tryLock(*os.File) (bool, error) { return true, nil }

func unlock(*os.File) error { return nil }

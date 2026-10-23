//go:build !unix && !windows

package capture

import (
	"os"
	"os/exec"
)

func detach(*exec.Cmd) {}

var devNull = os.DevNull

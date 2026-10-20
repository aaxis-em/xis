//go:build !unix

package capture

import (
	"os"
	"os/exec"
)

func detach(*exec.Cmd) {}

var devNull = os.DevNull

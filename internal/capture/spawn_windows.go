//go:build windows

package capture

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach starts the ingest without a console and outside the process group
// of Claude Code, so it keeps running after the hook returns.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

var devNull = os.DevNull

package capture

import (
	"os"
	"os/exec"
	"strings"
)

const NoSpawnEnv = "XIS_NO_ASYNC_INGEST"

func SpawnIngest(sessionID string) error {
	if os.Getenv(NoSpawnEnv) != "" {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.HasSuffix(exe, ".test") {
		return nil
	}
	null, err := os.OpenFile(devNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd := exec.Command(exe, "ingest", "--session", sessionID, "--quiet")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

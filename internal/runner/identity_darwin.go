//go:build darwin

package runner

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func processBirthIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrExecution
	}
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", fmt.Errorf("read process identity: %w", err)
	}
	started := process.Proc.P_starttime
	return fmt.Sprintf("darwin-starttime:%d:%d", started.Sec, started.Usec), nil
}

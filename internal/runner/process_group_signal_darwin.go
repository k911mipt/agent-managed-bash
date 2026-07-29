//go:build darwin

package runner

import (
	"errors"

	"golang.org/x/sys/unix"
)

const darwinProcessStateZombie = 5

func benignProcessGroupSignalError(processGroupID int, signalErr error) bool {
	if !errors.Is(signalErr, unix.EPERM) {
		return false
	}
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return false
	}
	for _, process := range processes {
		if process.Eproc.Pgid == int32(processGroupID) && process.Proc.P_stat != darwinProcessStateZombie {
			return false
		}
	}
	return true
}

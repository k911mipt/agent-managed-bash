//go:build linux || darwin

package runner

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func newSessionAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func verifyStartedProcessGroup(pid int) error {
	if pid <= 0 {
		return ErrExecution
	}
	processGroupID, err := unix.Getpgid(pid)
	if err != nil {
		return fmt.Errorf("read process group: %w", err)
	}
	if processGroupID != pid {
		return ErrExecution
	}
	return nil
}

func signalProcessGroup(processGroupID int, signal unix.Signal) error {
	if processGroupID <= 0 {
		return ErrExecution
	}
	if err := unix.Kill(-processGroupID, signal); err != nil && !errors.Is(err, unix.ESRCH) &&
		!benignProcessGroupSignalError(processGroupID, err) {
		return fmt.Errorf("signal process group %d: %w", processGroupID, err)
	}
	return nil
}

func VerifyProcessIdentity(pid int, expected string) (bool, error) {
	actual, err := processBirthIdentity(pid)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
			return false, nil
		}
		return false, err
	}
	return actual == expected, nil
}

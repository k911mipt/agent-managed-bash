//go:build darwin

package runner

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_signalProcessGroup_treats_zombie_group_as_gone(t *testing.T) {
	command := exec.Command("sh", "-c", "exit 0")
	command.SysProcAttr = newSessionAttributes()
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	})
	waitForCondition(t, time.Second, func() bool {
		process, err := unix.SysctlKinfoProc("kern.proc.pid", command.Process.Pid)
		return err == nil && process.Proc.P_stat == darwinProcessStateZombie
	})

	err := signalProcessGroup(command.Process.Pid, unix.SIGKILL)

	require.NoError(t, err)
	require.NoError(t, command.Wait())
}

func Test_benignProcessGroupSignalError_treats_disappeared_group_as_gone(t *testing.T) {
	const processGroupID = 999999
	require.ErrorIs(t, unix.Kill(-processGroupID, 0), unix.ESRCH)

	require.True(t, benignProcessGroupSignalError(processGroupID, unix.EPERM))
}

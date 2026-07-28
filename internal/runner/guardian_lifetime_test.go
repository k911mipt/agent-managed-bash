//go:build linux || darwin

package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_Guardian_remains_alive_after_shell_exit_until_group_signal(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "guardian-host")
	build := exec.Command("go", "build", "-o", executable, "./testdata/guardianhost")
	buildOutput, err := build.CombinedOutput()
	require.NoError(t, err, string(buildOutput))
	commandReader, commandWriter, err := os.Pipe()
	require.NoError(t, err)
	events, control, err := os.Pipe()
	require.NoError(t, err)
	lifetimeReader, lifetimeWriter, err := os.Pipe()
	require.NoError(t, err)
	cwd, err := openWorkspaceDirectory(testWorkspace(t))
	require.NoError(t, err)
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	require.NoError(t, err)
	guardian := exec.Command(executable, internalGuardianArgument)
	guardian.Stdin = devNull
	guardian.Stdout = devNull
	guardian.Stderr = devNull
	guardian.ExtraFiles = []*os.File{commandReader, control, cwd, lifetimeReader}
	guardian.SysProcAttr = newSessionAttributes()
	require.NoError(t, guardian.Start())
	waited := make(chan error, 1)
	go func() { waited <- guardian.Wait() }()
	cleanupDone := false
	t.Cleanup(func() {
		if cleanupDone {
			return
		}
		_ = lifetimeWriter.Close()
		_ = signalProcessGroup(guardian.Process.Pid, unix.SIGKILL)
		<-waited
	})
	require.NoError(t, commandReader.Close())
	require.NoError(t, control.Close())
	require.NoError(t, cwd.Close())
	require.NoError(t, lifetimeReader.Close())
	require.NoError(t, devNull.Close())
	require.NoError(t, writeFrame(commandWriter, frameStart, guardianStart{Command: "exit 0", GraceMs: 20}))
	require.NoError(t, commandWriter.Close())
	readyFrame, err := readFrame(events)
	require.NoError(t, err)
	var ready guardianReady
	require.NoError(t, decodeFrame(readyFrame, frameGuardianReady, &ready))
	exitedFrame, err := readFrame(events)
	require.NoError(t, err)
	var exited shellExit
	require.NoError(t, decodeFrame(exitedFrame, frameShellExited, &exited))
	require.NoError(t, events.Close())
	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()
	prematureExit := false
	var waitErr error
	select {
	case waitErr = <-waited:
		prematureExit = true
	case <-timer.C:
		require.NoError(t, signalProcessGroup(ready.ProcessGroupID, unix.SIGTERM))
		waitErr = <-waited
	}
	cleanupDone = true
	require.NoError(t, lifetimeWriter.Close())

	require.False(t, prematureExit, "guardian exited before runner cleanup signal: %v", waitErr)
	require.Error(t, waitErr)
}

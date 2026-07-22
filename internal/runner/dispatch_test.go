//go:build linux || darwin

package runner_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_DispatchInternal_preserves_fd3_for_ordinary_single_argument(t *testing.T) {
	// Given
	executable, err := os.Executable()
	require.NoError(t, err)
	readEnd, writeEnd, err := os.Pipe()
	require.NoError(t, err)
	defer readEnd.Close()
	command := exec.Command(executable, "-test.run=^Test_DispatchInternalOrdinaryHelper$")
	command.Env = append(os.Environ(), "AMB_ORDINARY_FD_HELPER=1")
	command.ExtraFiles = []*os.File{writeEnd}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	require.NoError(t, command.Start())
	require.NoError(t, writeEnd.Close())

	// When
	message, readErr := io.ReadAll(readEnd)
	waitErr := command.Wait()

	// Then
	require.NoError(t, readErr)
	require.NoError(t, waitErr, stderr.String())
	require.Equal(t, "usable", string(message))
}

func Test_DispatchInternalOrdinaryHelper(t *testing.T) {
	if os.Getenv("AMB_ORDINARY_FD_HELPER") != "1" {
		return
	}

	// When
	handled, err := runner.DispatchInternal(context.Background(), []string{"ordinary"})
	control := os.NewFile(3, "ordinary-caller-owned")
	require.NotNil(t, control)
	defer control.Close()
	_, writeErr := control.Write([]byte("usable"))

	// Then
	require.NoError(t, err)
	require.False(t, handled)
	require.NoError(t, writeErr)
}

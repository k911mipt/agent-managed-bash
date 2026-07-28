//go:build linux || darwin

package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_Manager_bounds_startup_failure_without_publishing_job_or_command_argv(t *testing.T) {
	// Given
	workspace := runner.NewTestWorkspace(t)
	invocation := trustedInvocation(t, workspace, workspace)
	argumentsPath := filepath.Join(t.TempDir(), "arguments")
	stalledExecutable := filepath.Join(t.TempDir(), "stalled-bootstrap")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$AMB_ARGUMENTS_PATH\"\nexec sleep 10\n"
	require.NoError(t, os.WriteFile(stalledExecutable, []byte(script), 0o700))
	t.Setenv("AMB_ARGUMENTS_PATH", argumentsPath)
	manager, err := runner.New(runner.Config{
		Executable: stalledExecutable, StartupTimeout: 75 * time.Millisecond,
		TerminationGrace: testTerminationGrace,
	})
	require.NoError(t, err)
	secretCommand := "printf command-must-not-be-an-argument"
	started := time.Now()

	// When
	_, err = manager.Start(context.Background(), runner.StartRequest{
		Invocation: invocation, Command: secretCommand, HardTimeout: time.Second, OutputLimitBytes: 1024,
	})
	elapsed := time.Since(started)
	arguments, argumentsErr := os.ReadFile(argumentsPath)
	_, managedErr := os.Stat(filepath.Join(workspace, ".managed_bash"))

	// Then
	require.ErrorIs(t, err, runner.ErrStartupTimeout)
	require.Less(t, elapsed, time.Second)
	require.NoError(t, argumentsErr)
	require.NotContains(t, string(arguments), secretCommand)
	require.ErrorIs(t, managedErr, os.ErrNotExist)
}

func Test_Manager_bounds_initial_frame_write_by_startup_timeout(t *testing.T) {
	workspace := runner.NewTestWorkspace(t)
	invocation := trustedInvocation(t, workspace, workspace)
	stalledExecutable := filepath.Join(t.TempDir(), "stalled-bootstrap")
	require.NoError(t, os.WriteFile(stalledExecutable, []byte("#!/bin/sh\nexec sleep 10\n"), 0o700))
	manager, err := runner.New(runner.Config{
		Executable: stalledExecutable, StartupTimeout: 50 * time.Millisecond, TerminationGrace: testTerminationGrace,
	})
	require.NoError(t, err)
	started := time.Now()

	_, err = manager.Start(context.Background(), runner.StartRequest{
		Invocation: invocation, Command: strings.Repeat("x", 65_500), HardTimeout: time.Second, OutputLimitBytes: 1024,
	})

	require.ErrorIs(t, err, runner.ErrStartupTimeout)
	require.Less(t, time.Since(started), time.Second)
}

func Test_Manager_pre_cancelled_start_does_not_spawn_or_publish(t *testing.T) {
	workspace := runner.NewTestWorkspace(t)
	marker := filepath.Join(workspace, "marker")
	invocation := trustedInvocation(t, workspace, workspace)
	manager, err := runner.New(runner.Config{})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = manager.Start(ctx, runner.StartRequest{
		Invocation: invocation, Command: "touch " + marker, HardTimeout: time.Second, OutputLimitBytes: 1024,
	})

	require.ErrorIs(t, err, context.Canceled)
	_, markerErr := os.Stat(marker)
	_, managedErr := os.Stat(filepath.Join(workspace, ".managed_bash"))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
	require.ErrorIs(t, managedErr, os.ErrNotExist)
}

func Test_Manager_classifies_missing_executable_as_startup_failure(t *testing.T) {
	// Given
	workspace := runner.NewTestWorkspace(t)
	invocation := trustedInvocation(t, workspace, workspace)
	manager, err := runner.New(runner.Config{Executable: filepath.Join(workspace, "missing-managed-bash")})
	require.NoError(t, err)

	// When
	_, err = manager.Start(context.Background(), runner.StartRequest{
		Invocation: invocation, Command: "printf unreachable", HardTimeout: time.Second, OutputLimitBytes: 1024,
	})

	// Then
	require.ErrorIs(t, err, runner.ErrStartupFailed)
}

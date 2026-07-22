//go:build linux || darwin

package runner_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_Manager_maps_real_shell_outcomes(t *testing.T) {
	tests := []struct {
		name           string
		command        string
		expectedStatus generated.JobStatus
		expectedOutput string
		expectedExit   *int
		expectedSignal *int
	}{
		{
			name: "success", command: "printf ok", expectedStatus: generated.JobStatusSucceeded,
			expectedOutput: "ok", expectedExit: intPointer(0),
		},
		{
			name: "nonzero exit", command: "printf bad; exit 7", expectedStatus: generated.JobStatusNonzeroExit,
			expectedOutput: "bad", expectedExit: intPointer(7),
		},
		{
			name: "signal exit", command: "kill -TERM $$", expectedStatus: generated.JobStatusSignalExit,
			expectedSignal: intPointer(15),
		},
		{
			name: "group signal before readiness", command: "kill -TERM 0", expectedStatus: generated.JobStatusSignalExit,
			expectedSignal: intPointer(15),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Given / When
			result := runLifecycle(t, testCase.command, 2*time.Second, 1<<20)

			// Then
			require.Equal(t, testCase.expectedStatus, result.snapshot.State.Job.Status)
			require.Equal(t, testCase.expectedOutput, string(result.output))
			require.Equal(t, testCase.expectedExit, result.snapshot.State.Result.ExitCode)
			require.Equal(t, testCase.expectedSignal, result.snapshot.State.Result.Signal)
		})
	}
}

func Test_Manager_inherits_environment_without_persisting_it(t *testing.T) {
	// Given
	secret := "in-memory-secret-7dc26"
	t.Setenv("AMB_LIFECYCLE_SECRET", secret)

	// When
	result := runLifecycle(t, `printf '%s' "$AMB_LIFECYCLE_SECRET"`, 2*time.Second, 1<<20)
	stateRaw, stateErr := os.ReadFile(filepath.Join(jobPath(result.workspace, result.job.JobID), "state.json"))
	runtimeRaw, runtimeErr := os.ReadFile(filepath.Join(jobPath(result.workspace, result.job.JobID), "runtime.json"))

	// Then
	require.Equal(t, secret, string(result.output))
	require.NoError(t, stateErr)
	require.NoError(t, runtimeErr)
	require.NotContains(t, string(stateRaw), secret)
	require.NotContains(t, string(runtimeRaw), secret)
}

func Test_Manager_captures_one_ordered_stdout_stderr_stream(t *testing.T) {
	// Given / When
	result := runLifecycle(t, `printf out; printf err >&2; printf end`, 2*time.Second, 1<<20)

	// Then
	require.Equal(t, "outerrend", string(result.output))
	require.Equal(t, generated.JobStatusSucceeded, result.snapshot.State.Job.Status)
}

func Test_Manager_terminates_at_exact_output_limit(t *testing.T) {
	// Given / When
	result := runLifecycle(t, `while :; do printf 0123456789abcdef; done`, 2*time.Second, 64)

	// Then
	require.Equal(t, generated.JobStatusOutputLimit, result.snapshot.State.Job.Status)
	require.Len(t, result.output, 64)
	require.Equal(t, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", string(result.output))
}

func Test_Manager_enforces_independent_hard_timeout(t *testing.T) {
	// Given / When
	result := runLifecycle(t, `trap '' TERM; while :; do :; done`, 80*time.Millisecond, 1<<20)

	// Then
	require.Equal(t, generated.JobStatusHardTimeout, result.snapshot.State.Job.Status)
	require.NotNil(t, result.snapshot.State.Result.Signal)
}

func Test_Manager_kills_term_ignoring_process_group_descendant_after_grace(t *testing.T) {
	// Given / When
	command := `trap '' TERM; (trap '' TERM; exec sleep 10) & child=$!; printf '%s' "$child"; wait`
	result := runLifecycle(t, command, time.Second, 1<<20)
	descendantPID, err := strconv.Atoi(string(result.output))
	require.NoError(t, err)

	// Then
	require.Equal(t, generated.JobStatusHardTimeout, result.snapshot.State.Job.Status)
	waitForProcessGone(t, descendantPID, time.Second)
}

func Test_Manager_cleans_descendant_after_normal_shell_exit_while_guardian_pins_group(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "pid")
	command := `(trap '' TERM; exec sleep 10) >/dev/null 2>&1 & printf '%s' "$!" >` + strconv.Quote(pidPath)

	result := runLifecycle(t, command, time.Second, 1<<20)
	descendantPID := readProcessID(t, pidPath)

	require.Equal(t, generated.JobStatusSucceeded, result.snapshot.State.Job.Status)
	waitForProcessGone(t, descendantPID, time.Second)
}

func Test_Manager_cleans_descendant_after_output_limit_while_guardian_pins_group(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "pid")
	command := `(trap '' TERM; exec sleep 10) >/dev/null 2>&1 & printf '%s' "$!" >` + strconv.Quote(pidPath) +
		`; while :; do printf 0123456789abcdef; done`

	result := runLifecycle(t, command, time.Second, 64)
	descendantPID := readProcessID(t, pidPath)

	require.Equal(t, generated.JobStatusOutputLimit, result.snapshot.State.Job.Status)
	waitForProcessGone(t, descendantPID, time.Second)
}

func Test_Manager_cleans_descendant_after_cancel_while_guardian_pins_group(t *testing.T) {
	workspace := t.TempDir()
	pidPath := filepath.Join(workspace, "pid")
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	command := `trap '' TERM; (trap '' TERM; exec sleep 10) >/dev/null 2>&1 & printf '%s' "$!" >` +
		strconv.Quote(pidPath) + `; wait`
	job := startControlJob(t, manager, owner, command)
	controlWaitForCondition(t, time.Second, func() bool {
		_, err := os.Stat(pidPath)
		return err == nil
	})
	descendantPID := readProcessID(t, pidPath)

	_, err := manager.Cancel(context.Background(), runner.CancelRequest{Invocation: owner, JobID: job.JobID})
	require.NoError(t, err)
	store := newStoreForInvocation(t, owner)
	snapshot := waitForTerminal(t, store, job.JobID, 2*time.Second)

	require.Equal(t, generated.JobStatusCancelled, snapshot.State.Job.Status)
	waitForProcessGone(t, descendantPID, time.Second)
}

func readProcessID(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	pid, err := strconv.Atoi(string(raw))
	require.NoError(t, err)
	return pid
}

func waitForProcessGone(t *testing.T, pid int, maximumWait time.Duration) {
	t.Helper()
	deadline := time.NewTimer(maximumWait)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := unix.Kill(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			return
		}
		require.NoError(t, err)
		select {
		case <-deadline.C:
			t.Fatalf("process %d still exists", pid)
		case <-ticker.C:
		}
	}
}

func intPointer(value int) *int {
	return &value
}

//go:build linux || darwin

package runner_test

import (
	"context"
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

func Test_Manager_runtime_identifies_live_unreaped_guardian_as_process_group_leader(t *testing.T) {
	workspace := t.TempDir()
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	job := startControlJob(t, manager, owner, `trap '' TERM; exec sleep 10`)
	store := newStoreForInvocation(t, owner)
	snapshot, err := store.Load(job.JobID)
	require.NoError(t, err)
	runtime := snapshot.Runtime

	processGroupID, err := unix.Getpgid(runtime.ProcessGroupID)
	require.NoError(t, err)
	matches, err := runner.VerifyProcessIdentity(runtime.ProcessGroupID, runtime.ProcessBirthIdentity)
	require.NoError(t, err)
	require.True(t, matches)
	require.Equal(t, runtime.ProcessGroupID, processGroupID)
	require.Equal(t, runtime.ProcessGroupLeaderPID, runtime.ProcessGroupID)
	require.NotEqual(t, runtime.ShellPID, runtime.ProcessGroupID)

	_, err = manager.Cancel(context.Background(), runner.CancelRequest{Invocation: owner, JobID: job.JobID})
	require.NoError(t, err)
	waitForTerminal(t, store, job.JobID, 2*time.Second)
}

func Test_Manager_runner_death_triggers_guardian_cleanup_without_observer_signal(t *testing.T) {
	workspace := t.TempDir()
	pidPath := filepath.Join(workspace, "descendant-pid")
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	command := `trap '' TERM; (trap '' TERM; exec sleep 10) >/dev/null 2>&1 & printf '%s' "$!" >` + strconv.Quote(pidPath) + `; wait`
	job := startControlJob(t, manager, owner, command)
	controlWaitForCondition(t, time.Second, func() bool {
		_, err := os.Stat(pidPath)
		return err == nil
	})
	store := newStoreForInvocation(t, owner)
	snapshot, err := store.Load(job.JobID)
	require.NoError(t, err)
	descendantPID := readProcessID(t, pidPath)

	require.NoError(t, unix.Kill(snapshot.Runtime.RunnerPID, unix.SIGKILL))
	waitForProcessGone(t, descendantPID, 2*time.Second)
	controlWaitForCondition(t, time.Second, func() bool {
		observation, statusErr := manager.Status(context.Background(), runner.StatusRequest{Invocation: owner, JobID: job.JobID})
		return statusErr == nil && observation.Job.Status == generated.JobStatusRunnerLost
	})

	observation, err := manager.Status(context.Background(), runner.StatusRequest{Invocation: owner, JobID: job.JobID})
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusRunnerLost, observation.Job.Status)
	require.Contains(t, *observation.ProcessResult.Diagnostic, "cleanup skipped")
}

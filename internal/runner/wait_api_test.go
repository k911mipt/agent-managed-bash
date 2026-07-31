//go:build linux || darwin

package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
)

func Test_Manager_wait_idle_and_absolute_checkpoints_never_terminate_job(t *testing.T) {
	// Given
	workspace := runner.NewTestWorkspace(t)
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	job := startControlJob(t, manager, owner, "trap '' TERM; exec sleep 10")

	// When
	idle, idleErr := manager.PrepareWait(context.Background(), runner.WaitRequest{
		Invocation: owner, JobID: job.JobID, Timeout: 500 * time.Millisecond, IdleTimeout: 30 * time.Millisecond,
	})
	require.NoError(t, idleErr)
	require.NoError(t, idle.Commit(context.Background()))
	repeated, repeatedErr := manager.PrepareWait(context.Background(), runner.WaitRequest{
		Invocation: owner, JobID: job.JobID, Timeout: 500 * time.Millisecond, IdleTimeout: 30 * time.Millisecond,
	})
	require.NoError(t, repeatedErr)
	require.NoError(t, repeated.Commit(context.Background()))
	started := time.Now()
	absolute, absoluteErr := manager.PrepareWait(context.Background(), runner.WaitRequest{
		Invocation: owner, JobID: job.JobID, Timeout: 40 * time.Millisecond, IdleTimeout: 500 * time.Millisecond,
	})

	// Then
	require.NoError(t, absoluteErr)
	require.Equal(t, generated.ObservationReasonOutputIdle, idle.Reason)
	require.Equal(t, generated.ObservationReasonOutputIdle, repeated.Reason)
	require.Equal(t, generated.ObservationReasonObservationTimeout, absolute.Reason)
	require.Empty(t, idle.Observation.Output.Text)
	require.Empty(t, repeated.Observation.Output.Text)
	require.Empty(t, absolute.Observation.Output.Text)
	require.Less(t, time.Since(started), 250*time.Millisecond)
	require.Equal(t, generated.JobStatusRunning, absolute.Observation.Observation.Job.Status)
	status, err := manager.Status(context.Background(), runner.StatusRequest{Invocation: owner, JobID: job.JobID})
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusRunning, status.Job.Status)
	require.Nil(t, status.ProcessResult)
	_, err = manager.Cancel(context.Background(), runner.CancelRequest{Invocation: owner, JobID: job.JobID})
	require.NoError(t, err)
}

func Test_Manager_wait_reports_terminal_reason_for_completed_job(t *testing.T) {
	// Given
	workspace := runner.NewTestWorkspace(t)
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	job := startControlJob(t, manager, owner, "printf done")
	waitForTerminal(t, newStoreForInvocation(t, owner), job.JobID, time.Second)

	// When
	prepared, err := manager.PrepareWait(context.Background(), runner.WaitRequest{
		Invocation: owner, JobID: job.JobID, Timeout: 500 * time.Millisecond, IdleTimeout: 20 * time.Millisecond,
	})

	// Then
	require.NoError(t, err)
	require.Equal(t, generated.ObservationReasonTerminal, prepared.Reason)
	require.Equal(t, generated.JobStatusSucceeded, prepared.Observation.Observation.Job.Status)
	require.Equal(t, "done", prepared.Observation.Output.Text)
}

func Test_Manager_wait_delivers_output_and_commits_cursor_monotonically(t *testing.T) {
	// Given
	workspace := runner.NewTestWorkspace(t)
	releasePath := filepath.Join(workspace, "release")
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	observer := trustedInvocationFor(t, "observer", workspace)
	job := startControlJob(t, manager, owner, `printf abc; while [ ! -f `+releasePath+` ]; do sleep 0.01; done`)
	waitCaptured(t, owner, job.JobID, 3)

	// When
	first, err := manager.PrepareWait(context.Background(), runner.WaitRequest{
		Invocation: observer, JobID: job.JobID, Timeout: 500 * time.Millisecond, IdleTimeout: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	duplicate, err := manager.PrepareWait(context.Background(), runner.WaitRequest{
		Invocation: observer, JobID: job.JobID, Timeout: 500 * time.Millisecond, IdleTimeout: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	first.Observation.Output.NextCursorBytes = 0
	first.Observation.Output.CapturedBytes = 0
	require.NoError(t, first.Commit(context.Background()))
	zero := generated.ByteCursor(0)
	replayed, err := manager.PrepareWait(context.Background(), runner.WaitRequest{
		Invocation: observer, JobID: job.JobID, CursorBytes: &zero,
		Timeout: 500 * time.Millisecond, IdleTimeout: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	require.NoError(t, replayed.Commit(context.Background()))
	store := newStoreForInvocation(t, owner)
	snapshot, loadErr := store.Load(job.JobID)

	// Then
	require.Equal(t, "abc", first.Observation.Output.Text)
	require.Equal(t, "abc", duplicate.Observation.Output.Text)
	require.Equal(t, "abc", replayed.Observation.Output.Text)
	require.NoError(t, loadErr)
	cursor := observerCursor(snapshot, "observer")
	require.NotNil(t, cursor)
	require.Equal(t, generated.ByteCursor(3), cursor.CursorBytes)
	require.NoError(t, os.WriteFile(releasePath, []byte("release"), 0o600))
}

func Test_Manager_wait_commit_failure_preserves_duplicate_delivery(t *testing.T) {
	// Given
	workspace := runner.NewTestWorkspace(t)
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	job := startControlJob(t, manager, owner, "printf payload")
	waitCaptured(t, owner, job.JobID, len("payload"))
	prepared, err := manager.PrepareWait(context.Background(), runner.WaitRequest{
		Invocation: owner, JobID: job.JobID, Timeout: 500 * time.Millisecond, IdleTimeout: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	original := jobPath(workspace, job.JobID)
	moved := original + ".moved"
	require.NoError(t, os.Rename(original, moved))

	// When
	commitErr := prepared.Commit(context.Background())
	require.NoError(t, os.Rename(moved, original))
	duplicate, duplicateErr := manager.PrepareWait(context.Background(), runner.WaitRequest{
		Invocation: owner, JobID: job.JobID, Timeout: 500 * time.Millisecond, IdleTimeout: 20 * time.Millisecond,
	})

	// Then
	require.ErrorIs(t, commitErr, runner.ErrJobNotFound)
	require.NoError(t, duplicateErr)
	require.Equal(t, "payload", prepared.Observation.Output.Text)
	require.Equal(t, "payload", duplicate.Observation.Output.Text)
}

func newStoreForInvocation(t *testing.T, invocation state.TrustedInvocation) *runner.Store {
	t.Helper()
	contracts, err := contract.Load()
	require.NoError(t, err)
	store, err := runner.OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

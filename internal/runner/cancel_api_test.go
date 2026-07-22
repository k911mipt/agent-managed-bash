//go:build linux || darwin

package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_Manager_cancel_is_persisted_polled_and_idempotent(t *testing.T) {
	// Given
	workspace := t.TempDir()
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	job := startControlJob(t, manager, owner, "trap '' TERM; exec sleep 10")

	// When
	first, firstErr := manager.Cancel(context.Background(), runner.CancelRequest{Invocation: owner, JobID: job.JobID})
	repeated, repeatedErr := manager.Cancel(context.Background(), runner.CancelRequest{Invocation: owner, JobID: job.JobID})
	store := newStoreForInvocation(t, owner)
	terminal := waitForTerminal(t, store, job.JobID, testLifecycleDeadline)
	rawState, readErr := os.ReadFile(filepath.Join(jobPath(workspace, job.JobID), "state.json"))

	// Then
	require.NoError(t, firstErr)
	require.Equal(t, generated.CancellationOutcomeRequested, first.Outcome)
	require.NotNil(t, first.Cancellation)
	require.NoError(t, repeatedErr)
	require.Equal(t, generated.CancellationOutcomeAlreadyRequested, repeated.Outcome)
	require.Equal(t, first.Cancellation, repeated.Cancellation)
	require.Equal(t, generated.JobStatusCancelled, terminal.State.Job.Status)
	require.Equal(t, generated.TerminalStatusCancelled, terminal.State.Result.Status)
	require.Equal(t, first.Cancellation, terminal.State.Cancellation)
	require.NoError(t, readErr)
	require.NotContains(t, string(rawState), "poll_interval")
}

func Test_Manager_cancel_terminal_job_is_noop(t *testing.T) {
	// Given
	workspace := t.TempDir()
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	job := startControlJob(t, manager, owner, "printf done")
	store := newStoreForInvocation(t, owner)
	waitForTerminal(t, store, job.JobID, testLifecycleDeadline)

	// When
	result, err := manager.Cancel(context.Background(), runner.CancelRequest{Invocation: owner, JobID: job.JobID})

	// Then
	require.NoError(t, err)
	require.Equal(t, generated.CancellationOutcomeAlreadyTerminal, result.Outcome)
	require.Equal(t, generated.JobStatusSucceeded, result.Status)
	require.Nil(t, result.Cancellation)
}

func Test_Manager_cancel_rejects_non_owner(t *testing.T) {
	// Given
	workspace := t.TempDir()
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	other := trustedInvocationFor(t, "other", workspace)
	job := startControlJob(t, manager, owner, "trap '' TERM; exec sleep 10")

	// When
	_, err := manager.Cancel(context.Background(), runner.CancelRequest{Invocation: other, JobID: job.JobID})

	// Then
	require.ErrorIs(t, err, runner.ErrUnauthorized)
	_, cancelErr := manager.Cancel(context.Background(), runner.CancelRequest{Invocation: owner, JobID: job.JobID})
	require.NoError(t, cancelErr)
}

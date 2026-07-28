//go:build linux || darwin

package runner_test

import (
	"context"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_Manager_observation_and_control_authorization(t *testing.T) {
	// Given
	workspace := runner.NewTestWorkspace(t)
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	observer := trustedInvocationFor(t, "observer", workspace)
	job := startControlJob(t, manager, owner, "printf abc")
	contractsStore := newStoreForInvocation(t, owner)
	waitForTerminal(t, contractsStore, job.JobID, testLifecycleDeadline)
	start := generated.ByteCursor(1)
	end := generated.ByteCursor(3)

	// When
	status, statusErr := manager.Status(context.Background(), runner.StatusRequest{Invocation: observer, JobID: job.JobID})
	output, outputErr := manager.Output(context.Background(), runner.OutputRequest{
		Invocation: observer, JobID: job.JobID, StartCursorBytes: &start, EndCursorBytes: &end,
	})
	listed, listErr := manager.List(context.Background(), runner.ListRequest{Invocation: observer})
	_, nonOwnerRemoveErr := manager.Remove(context.Background(), runner.RemoveRequest{Invocation: observer, JobID: job.JobID})
	removed, removeErr := manager.Remove(context.Background(), runner.RemoveRequest{Invocation: owner, JobID: job.JobID})

	// Then
	require.NoError(t, statusErr)
	require.Equal(t, generated.JobStatusSucceeded, status.Job.Status)
	require.NoError(t, outputErr)
	require.Equal(t, "bc", output.Output.Text)
	require.NoError(t, listErr)
	require.Len(t, listed.Jobs, 1)
	require.ErrorIs(t, nonOwnerRemoveErr, runner.ErrUnauthorized)
	require.NoError(t, removeErr)
	require.Equal(t, generated.RemoveResult{JobID: job.JobID, Removed: true}, removed)
}

func Test_Manager_cross_workspace_read_is_masked_as_not_found(t *testing.T) {
	// Given
	manager := newControlManager(t)
	ownerWorkspace := runner.NewTestWorkspace(t)
	otherWorkspace := runner.NewTestWorkspace(t)
	owner := trustedInvocationFor(t, "owner", ownerWorkspace)
	other := trustedInvocationFor(t, "other", otherWorkspace)
	job := startControlJob(t, manager, owner, "printf ok")

	// When
	_, err := manager.Status(context.Background(), runner.StatusRequest{Invocation: other, JobID: job.JobID})

	// Then
	require.ErrorIs(t, err, runner.ErrJobNotFound)
}

//go:build linux || darwin

package runner_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_Manager_concurrent_observation_wait_and_cancel_are_race_safe(t *testing.T) {
	// Given
	workspace := runner.NewTestWorkspace(t)
	manager := newControlManager(t)
	owner := trustedInvocationFor(t, "owner", workspace)
	observer := trustedInvocationFor(t, "observer", workspace)
	job := startControlJob(t, manager, owner, `trap '' TERM; while :; do printf x; sleep 0.01; done`)
	waitCaptured(t, owner, job.JobID, 1)

	// When
	errorsFound := make(chan error, 16)
	var group sync.WaitGroup
	for range 4 {
		group.Add(4)
		go func() {
			defer group.Done()
			_, err := manager.Status(context.Background(), runner.StatusRequest{Invocation: observer, JobID: job.JobID})
			errorsFound <- err
		}()
		go func() {
			defer group.Done()
			_, err := manager.Output(context.Background(), runner.OutputRequest{Invocation: observer, JobID: job.JobID})
			errorsFound <- err
		}()
		go func() {
			defer group.Done()
			prepared, err := manager.PrepareWait(context.Background(), runner.WaitRequest{
				Invocation: observer, JobID: job.JobID,
				Timeout: 100 * time.Millisecond, IdleTimeout: 30 * time.Millisecond,
			})
			if err == nil {
				err = prepared.Commit(context.Background())
			}
			errorsFound <- err
		}()
		go func() {
			defer group.Done()
			_, err := manager.Cancel(context.Background(), runner.CancelRequest{Invocation: owner, JobID: job.JobID})
			errorsFound <- err
		}()
	}
	group.Wait()
	close(errorsFound)
	store := newStoreForInvocation(t, owner)
	terminal := waitForTerminal(t, store, job.JobID, testLifecycleDeadline)

	// Then
	for err := range errorsFound {
		require.NoError(t, err)
	}
	require.Equal(t, generated.JobStatusCancelled, terminal.State.Job.Status)
}

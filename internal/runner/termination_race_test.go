//go:build linux || darwin

package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_PublishExecutionTerminal_persisted_cancellation_wins_internal_cause(t *testing.T) {
	for _, cause := range []executionCause{causeHardTimeout, causeOutputLimit} {
		t.Run(string(processStatusForCause(cause)), func(t *testing.T) {
			// Given
			store, initial, lease := newInternalTestJob(t)
			_, err := store.cancel(context.Background(), initial.Job.JobID)
			require.NoError(t, err)
			exitCode := 0
			outcome := executionOutcome{cause: cause, wait: shellWaitResult{exitCode: &exitCode}}

			// When
			err = store.publishExecutionTerminal(initial.Job.JobID, outcome, lease)
			snapshot, loadErr := store.Load(initial.Job.JobID)

			// Then
			require.NoError(t, err)
			require.NoError(t, loadErr)
			require.Equal(t, generated.JobStatusCancelled, snapshot.State.Job.Status)
			require.Equal(t, generated.TerminalStatusCancelled, snapshot.State.Result.Status)
		})
	}
}

func Test_cancel_serializes_with_terminal_publication(t *testing.T) {
	// Given
	store, initial, lease := newInternalTestJob(t)
	t.Cleanup(func() { require.NoError(t, lease.release()) })
	cancelLocked := make(chan struct{})
	releaseCancel := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseCancel) }) })
	terminalWaiting := make(chan struct{})
	store.afterRecoveryLock = func() {
		close(cancelLocked)
		<-releaseCancel
	}
	store.beforeTerminalRecoveryLock = func() { close(terminalWaiting) }
	type cancelResult struct {
		result generated.CancellationResult
		err    error
	}
	cancelled := make(chan cancelResult, 1)
	go func() {
		result, err := store.cancel(context.Background(), initial.Job.JobID)
		cancelled <- cancelResult{result: result, err: err}
	}()
	receiveRunnerRaceResult(t, cancelLocked)
	exitCode := 0
	published := make(chan error, 1)
	go func() {
		published <- store.publishExecutionTerminal(initial.Job.JobID, executionOutcome{
			cause: causeNormal, wait: shellWaitResult{exitCode: &exitCode},
		}, lease)
	}()
	receiveRunnerRaceResult(t, terminalWaiting)

	// When
	releaseOnce.Do(func() { close(releaseCancel) })
	cancellation := receiveRunnerRaceResult(t, cancelled)
	publishErr := receiveRunnerRaceResult(t, published)
	snapshot, loadErr := store.Load(initial.Job.JobID)

	// Then
	require.NoError(t, cancellation.err)
	require.Equal(t, generated.CancellationOutcomeRequested, cancellation.result.Outcome)
	require.NoError(t, publishErr)
	require.NoError(t, loadErr)
	require.NotNil(t, snapshot.State.Cancellation)
	require.Equal(t, generated.JobStatusCancelled, snapshot.State.Job.Status)
	require.Equal(t, generated.TerminalStatusCancelled, snapshot.State.Result.Status)
}

func Test_cancel_active_runner_ignores_legacy_terminal_intent_while_holding_recovery_lock(t *testing.T) {
	// Given
	store, initial, lease := newInternalTestJob(t)
	t.Cleanup(func() { require.NoError(t, lease.release()) })
	store.beforeActiveMutation = func() {
		require.NoError(t, store.createTerminalIntent(initial.Job.JobID))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// When
	result, err := store.cancel(ctx, initial.Job.JobID)

	// Then
	require.NoError(t, err)
	require.Equal(t, generated.CancellationOutcomeRequested, result.Outcome)
	require.NoError(t, store.clearTerminalIntent(initial.Job.JobID))
	exitCode := 0
	require.NoError(t, store.publishExecutionTerminal(initial.Job.JobID, executionOutcome{
		cause: causeNormal, wait: shellWaitResult{exitCode: &exitCode},
	}, lease))
	snapshot, loadErr := store.Load(initial.Job.JobID)
	require.NoError(t, loadErr)
	require.Equal(t, generated.JobStatusCancelled, snapshot.State.Job.Status)
}

func receiveRunnerRaceResult[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("runner race did not reach the expected synchronization point")
		var zero T
		return zero
	}
}

func processStatusForCause(cause executionCause) generated.TerminalStatus {
	if cause == causeHardTimeout {
		return generated.TerminalStatusHardTimeout
	}
	return generated.TerminalStatusOutputLimit
}

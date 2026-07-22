//go:build linux || darwin

package runner

import (
	"context"
	"testing"

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

func processStatusForCause(cause executionCause) generated.TerminalStatus {
	if cause == causeHardTimeout {
		return generated.TerminalStatusHardTimeout
	}
	return generated.TerminalStatusOutputLimit
}

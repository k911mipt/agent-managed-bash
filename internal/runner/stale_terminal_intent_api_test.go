//go:build linux || darwin

package runner

import (
	"context"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_Manager_cancel_clears_stale_terminal_intent_and_preserves_output_limit_result(t *testing.T) {
	fixture := newStaleTerminalIntentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := fixture.manager.Cancel(ctx, CancelRequest{
		Invocation: fixture.invocation,
		JobID:      fixture.jobID,
	})

	require.NoError(t, err)
	require.Equal(t, generated.CancellationOutcomeAlreadyTerminal, result.Outcome)
	require.Equal(t, generated.JobStatusOutputLimit, result.Status)
	fixture.requireOriginalTerminalState(t)
	fixture.requireIntentRemoved(t)
}

func Test_PreparedWait_commit_clears_stale_terminal_intent_once_and_is_idempotent(t *testing.T) {
	fixture := newStaleTerminalIntentFixture(t)
	prepared, err := fixture.manager.PrepareWait(context.Background(), WaitRequest{
		Invocation:  fixture.invocation,
		JobID:       fixture.jobID,
		Timeout:     100 * time.Millisecond,
		IdleTimeout: time.Millisecond,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	firstErr := prepared.Commit(ctx)
	secondErr := prepared.Commit(ctx)

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	fixture.requireOriginalTerminalState(t)
	snapshot, err := fixture.store.Load(fixture.jobID)
	require.NoError(t, err)
	require.Len(t, snapshot.State.Observers, 1)
	require.Equal(t, fixture.invocation.SessionID(), snapshot.State.Observers[0].SessionID)
	require.Equal(t, prepared.Observation.Output.NextCursorBytes, snapshot.State.Observers[0].CursorBytes)
	fixture.requireIntentRemoved(t)
}

func Test_waitForTerminalIntent_returns_lock_timeout_when_reconciliation_deadline_expired(t *testing.T) {
	fixture := newStaleTerminalIntentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := fixture.store.waitForTerminalIntent(ctx, fixture.jobID, time.Now().Add(-time.Millisecond))

	require.ErrorIs(t, err, ErrStateLockTimeout)
}

type staleTerminalIntentFixture struct {
	manager        *Manager
	store          *Store
	invocation     state.TrustedInvocation
	jobID          generated.JobID
	originalResult generated.ProcessResult
}

func newStaleTerminalIntentFixture(t *testing.T) staleTerminalIntentFixture {
	t.Helper()
	store, initial, lease := newInternalTestJob(t)
	appendResult, err := store.appendOutput(initial.Job.JobID, make([]byte, initial.Job.OutputLimitBytes))
	require.NoError(t, err)
	require.True(t, appendResult.LimitReached)
	directory, err := openDirectoryAt(store.jobs, string(initial.Job.JobID), true)
	require.NoError(t, err)
	require.NoError(t, createSyncedFile(directory, terminalIntentName))
	require.NoError(t, directory.Sync())
	require.NoError(t, directory.Close())
	job, err := store.openLockedJobWith(initial.Job.JobID, lockStateFileBlocking)
	require.NoError(t, err)
	signal := int(unix.SIGKILL)
	terminal, err := terminalState(job.state, executionOutcome{
		cause: causeOutputLimit,
		wait:  shellWaitResult{signal: &signal},
	})
	require.NoError(t, err)
	require.NoError(t, store.publishStateLocked(job, initial.Job.JobID, terminal))
	require.NoError(t, job.close())
	require.NoError(t, lease.release())
	invocation, decision := store.contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: store.sessionID, WorkspacePath: store.workspace, Cwd: store.cwd},
		generated.TrustedContext{SessionID: store.sessionID, WorkspacePath: store.workspace, Cwd: store.cwd},
	)
	require.True(t, decision.Allowed)
	manager, err := New(Config{StateLockTimeout: time.Second, StateLockPoll: time.Millisecond, PollInterval: time.Millisecond})
	require.NoError(t, err)
	return staleTerminalIntentFixture{
		manager: manager, store: store, invocation: invocation, jobID: initial.Job.JobID,
		originalResult: *terminal.Result,
	}
}

func (fixture staleTerminalIntentFixture) requireOriginalTerminalState(t *testing.T) {
	t.Helper()
	snapshot, err := fixture.store.Load(fixture.jobID)
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusOutputLimit, snapshot.State.Job.Status)
	require.Equal(t, fixture.originalResult, *snapshot.State.Result)
}

func (fixture staleTerminalIntentFixture) requireIntentRemoved(t *testing.T) {
	t.Helper()
	directory, err := openDirectoryAt(fixture.store.jobs, string(fixture.jobID), true)
	require.NoError(t, err)
	exists, err := terminalIntentExists(directory)
	require.NoError(t, err)
	require.NoError(t, directory.Close())
	require.False(t, exists)
}

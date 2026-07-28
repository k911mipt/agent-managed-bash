//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_stateWriters_wait_for_terminal_intent_and_preserve_terminal_result(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, *Store, generated.PersistedJobState) func(context.Context) error
		wantError error
	}{
		{
			name: "cancel",
			prepare: func(_ *testing.T, store *Store, initial generated.PersistedJobState) func(context.Context) error {
				return func(ctx context.Context) error {
					_, err := store.cancel(ctx, initial.Job.JobID)
					return err
				}
			},
			wantError: context.DeadlineExceeded,
		},
		{
			name: "append output",
			prepare: func(_ *testing.T, store *Store, initial generated.PersistedJobState) func(context.Context) error {
				return func(context.Context) error {
					_, err := store.appendOutput(initial.Job.JobID, []byte("late"))
					return err
				}
			},
			wantError: ErrStateLockTimeout,
		},
		{
			name:      "prepared wait commit",
			prepare:   prepareWaitIntentWriter,
			wantError: context.DeadlineExceeded,
		},
		{
			name: "generic state publish",
			prepare: func(t *testing.T, store *Store, initial generated.PersistedJobState) func(context.Context) error {
				snapshot, err := store.Load(initial.Job.JobID)
				require.NoError(t, err)
				next := snapshot.State
				next.Observers = []generated.ObserverCursor{{SessionID: "observer", UpdatedAtUnixMs: 103}}
				return func(context.Context) error {
					return store.publishState(initial.Job.JobID, next)
				}
			},
			wantError: ErrStateLockTimeout,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store, initial, lease := newInternalTestJob(t)
			t.Cleanup(func() { require.NoError(t, lease.release()) })
			store.lockTimeout = 20 * time.Millisecond
			store.lockPoll = time.Millisecond
			appendResult, err := store.appendOutput(initial.Job.JobID, make([]byte, initial.Job.OutputLimitBytes))
			require.NoError(t, err)
			require.True(t, appendResult.LimitReached)
			writeState := testCase.prepare(t, store, initial)
			publisherBlocked, releasePublisher, published := pauseOutputLimitPublisher(store, initial, lease)
			<-publisherBlocked
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)

			writerErr := writeState(ctx)
			cancel()
			close(releasePublisher)

			require.ErrorIs(t, writerErr, testCase.wantError)
			require.NoError(t, <-published)
			snapshot, err := store.Load(initial.Job.JobID)
			require.NoError(t, err)
			require.Equal(t, generated.JobStatusOutputLimit, snapshot.State.Job.Status)
			require.Equal(t, generated.TerminalStatusOutputLimit, snapshot.State.Result.Status)
		})
	}
}

func Test_removeTerminal_rejects_live_terminal_publisher_then_removes_terminal_job(t *testing.T) {
	store, initial, lease := newInternalTestJob(t)
	t.Cleanup(func() { require.NoError(t, lease.release()) })
	appendResult, err := store.appendOutput(initial.Job.JobID, make([]byte, initial.Job.OutputLimitBytes))
	require.NoError(t, err)
	require.True(t, appendResult.LimitReached)
	publisherBlocked, releasePublisher, published := pauseOutputLimitPublisher(store, initial, lease)
	<-publisherBlocked

	removeWhilePublishingErr := store.removeTerminal(context.Background(), initial.Job.JobID)
	close(releasePublisher)
	publishErr := <-published
	snapshot, loadErr := store.Load(initial.Job.JobID)
	removeTerminalErr := store.removeTerminal(context.Background(), initial.Job.JobID)
	_, missingErr := store.Load(initial.Job.JobID)

	require.ErrorIs(t, removeWhilePublishingErr, ErrActiveJob)
	require.NoError(t, publishErr)
	require.NoError(t, loadErr)
	require.Equal(t, generated.JobStatusOutputLimit, snapshot.State.Job.Status)
	require.NoError(t, removeTerminalErr)
	require.ErrorIs(t, missingErr, ErrJobNotFound)
}

func Test_stateWriter_rechecks_terminal_intent_after_acquiring_state_lock(t *testing.T) {
	store, initial, lease := newInternalTestJob(t)
	t.Cleanup(func() { require.NoError(t, lease.release()) })
	appendResult, err := store.appendOutput(initial.Job.JobID, make([]byte, initial.Job.OutputLimitBytes))
	require.NoError(t, err)
	require.True(t, appendResult.LimitReached)
	snapshot, err := store.Load(initial.Job.JobID)
	require.NoError(t, err)
	next := snapshot.State
	next.Observers = []generated.ObserverCursor{{SessionID: "observer", UpdatedAtUnixMs: 103}}
	writerLocked := make(chan struct{})
	releaseWriter := make(chan struct{})
	store.afterMutationLock = func() {
		store.afterMutationLock = nil
		close(writerLocked)
		<-releaseWriter
	}
	writerResult := make(chan error, 1)
	go func() { writerResult <- store.publishState(initial.Job.JobID, next) }()
	<-writerLocked
	publisherBlocked, releasePublisher, published := pauseOutputLimitPublisher(store, initial, lease)
	<-publisherBlocked

	close(releaseWriter)
	close(releasePublisher)
	writerErr := <-writerResult
	publishErr := <-published

	require.ErrorIs(t, writerErr, ErrInvalidStateUpdate)
	require.NoError(t, publishErr)
	terminal, err := store.Load(initial.Job.JobID)
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusOutputLimit, terminal.State.Job.Status)
	require.Equal(t, generated.TerminalStatusOutputLimit, terminal.State.Result.Status)
}

func Test_removeTerminal_removes_parent_after_locked_job_close_failure(t *testing.T) {
	store, initial, lease := newInternalTestJob(t)
	exitCode := 0
	require.NoError(t, store.publishExecutionTerminal(initial.Job.JobID, executionOutcome{
		cause: causeNormal, wait: shellWaitResult{exitCode: &exitCode},
	}, lease))
	closeFailure := errors.New("injected locked job close failure")
	store.closeJob = func(file *os.File) error {
		closeErr := file.Close()
		if file.Name() == string(initial.Job.JobID) {
			return errors.Join(closeErr, closeFailure)
		}
		return closeErr
	}

	removeErr := store.removeTerminal(context.Background(), initial.Job.JobID)
	_, statErr := os.Stat(filepath.Join(store.workspace, ".managed_bash", "jobs", string(initial.Job.JobID)))

	require.ErrorIs(t, removeErr, closeFailure)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func Test_Manager_status_recovers_stale_terminal_intent_after_runner_loss(t *testing.T) {
	// Given
	store, initial, lease := newInternalTestJob(t)
	directory, err := openDirectoryAt(store.jobs, string(initial.Job.JobID), true)
	require.NoError(t, err)
	require.NoError(t, createSyncedFile(directory, terminalIntentName))
	require.NoError(t, directory.Sync())
	require.NoError(t, directory.Close())
	require.NoError(t, lease.release())

	// When
	observation, err := store.status(context.Background(), initial.Job.JobID)

	// Then
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusRunnerLost, observation.Job.Status)
	directory, err = openDirectoryAt(store.jobs, string(initial.Job.JobID), true)
	require.NoError(t, err)
	intentExists, err := entryExists(directory, terminalIntentName)
	require.NoError(t, err)
	require.NoError(t, directory.Close())
	require.False(t, intentExists)
}

func prepareWaitIntentWriter(t *testing.T, store *Store, initial generated.PersistedJobState) func(context.Context) error {
	t.Helper()
	invocation, decision := store.contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: store.sessionID, WorkspacePath: store.workspace, Cwd: store.cwd},
		generated.TrustedContext{SessionID: store.sessionID, WorkspacePath: store.workspace, Cwd: store.cwd},
	)
	require.True(t, decision.Allowed)
	manager, err := New(Config{StateLockTimeout: time.Second, StateLockPoll: time.Millisecond, PollInterval: time.Millisecond})
	require.NoError(t, err)
	prepared, err := manager.PrepareWait(context.Background(), WaitRequest{
		Invocation: invocation, JobID: initial.Job.JobID, Timeout: 100 * time.Millisecond, IdleTimeout: time.Millisecond,
	})
	require.NoError(t, err)
	return prepared.Commit
}

func pauseOutputLimitPublisher(
	store *Store,
	initial generated.PersistedJobState,
	lease *runnerLease,
) (<-chan struct{}, chan<- struct{}, <-chan error) {
	publisherBlocked := make(chan struct{})
	releasePublisher := make(chan struct{})
	store.afterTerminalIntent = func() {
		close(publisherBlocked)
		<-releasePublisher
	}
	signal := int(unix.SIGKILL)
	published := make(chan error, 1)
	go func() {
		published <- store.publishExecutionTerminal(initial.Job.JobID, executionOutcome{
			cause: causeOutputLimit, wait: shellWaitResult{signal: &signal},
		}, lease)
	}()
	return publisherBlocked, releasePublisher, published
}

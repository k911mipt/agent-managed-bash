//go:build linux || darwin

package runner

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_publishExecutionTerminal_waits_for_state_lock_instead_of_abandoning_running_state(t *testing.T) {
	// Given
	store, initial, lease := newInternalTestJob(t)
	appendResult, err := store.appendOutput(initial.Job.JobID, make([]byte, initial.Job.OutputLimitBytes))
	require.NoError(t, err)
	require.True(t, appendResult.LimitReached)
	store.lockTimeout = 10 * time.Millisecond
	store.lockPoll = time.Millisecond
	releaseObserver := holdObservedStateLock(t, store, initial.Job.JobID)
	t.Cleanup(releaseObserver)
	_, err = store.openLockedJob(initial.Job.JobID)
	require.ErrorIs(t, err, ErrStateLockTimeout)
	terminalLockStarted := make(chan struct{})
	store.acquireTerminalStateLock = func(file *os.File) error {
		close(terminalLockStarted)
		return lockStateFileBlocking(file)
	}
	signal := int(unix.SIGKILL)
	result := make(chan error, 1)
	go func() {
		result <- store.publishExecutionTerminal(initial.Job.JobID, executionOutcome{
			cause: causeOutputLimit, wait: shellWaitResult{signal: &signal},
		}, lease)
	}()
	<-terminalLockStarted

	// When
	select {
	case err := <-result:
		t.Fatalf("terminal publication returned while observer holds state lock: %v", err)
	default:
	}
	releaseObserver()

	// Then
	require.NoError(t, <-result)
	snapshot, err := store.Load(initial.Job.JobID)
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusOutputLimit, snapshot.State.Job.Status)
	require.Equal(t, generated.TerminalStatusOutputLimit, snapshot.State.Result.Status)
	require.Equal(t, generated.ByteCursor(initial.Job.OutputLimitBytes), snapshot.State.Job.CapturedBytes)
	require.Equal(t, generated.ByteCursor(initial.Job.OutputLimitBytes), snapshot.State.Result.CapturedBytes)
	require.Equal(t, &signal, snapshot.State.Result.Signal)
}

func Test_publishExecutionTerminal_completes_with_aggressive_load_observers(t *testing.T) {
	// Given
	store, initial, lease := newInternalTestJob(t)
	t.Cleanup(func() { require.NoError(t, lease.release()) })
	appendResult, err := store.appendOutput(initial.Job.JobID, make([]byte, initial.Job.OutputLimitBytes))
	require.NoError(t, err)
	require.True(t, appendResult.LimitReached)
	const observers = 16
	const loadsPerObserver = 64
	ready := make(chan struct{}, observers)
	start := make(chan struct{})
	observerResults := make(chan error, observers)
	var observerGroup sync.WaitGroup
	for range observers {
		observerGroup.Add(1)
		go func() {
			defer observerGroup.Done()
			ready <- struct{}{}
			<-start
			for range loadsPerObserver {
				if _, err := store.Load(initial.Job.JobID); err != nil {
					observerResults <- err
					return
				}
			}
			observerResults <- nil
		}()
	}
	for range observers {
		<-ready
	}
	signal := int(unix.SIGKILL)
	terminalResult := make(chan error, 1)
	go func() {
		<-start
		terminalResult <- store.publishExecutionTerminal(initial.Job.JobID, executionOutcome{
			cause: causeOutputLimit, wait: shellWaitResult{signal: &signal},
		}, lease)
	}()

	// When
	close(start)
	observerGroup.Wait()

	// Then
	for range observers {
		require.NoError(t, <-observerResults)
	}
	require.NoError(t, <-terminalResult)
	snapshot, err := store.Load(initial.Job.JobID)
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusOutputLimit, snapshot.State.Job.Status)
	require.Equal(t, generated.ByteCursor(initial.Job.OutputLimitBytes), snapshot.State.Result.CapturedBytes)
}

func holdObservedStateLock(t *testing.T, store *Store, jobID generated.JobID) func() {
	t.Helper()
	directory, err := openDirectoryAt(store.jobs, string(jobID), true)
	require.NoError(t, err)
	stateLock, err := openPrivateFileAt(directory, "state.lock", unix.O_RDWR)
	require.NoError(t, err)
	require.NoError(t, lockFile(stateLock, false))
	var releaseOnce sync.Once
	return func() {
		releaseOnce.Do(func() {
			require.NoError(t, unlockFile(stateLock))
			require.NoError(t, stateLock.Close())
			require.NoError(t, directory.Close())
		})
	}
}

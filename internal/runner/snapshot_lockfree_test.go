//go:build linux || darwin

package runner

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_Store_Load_returns_atomic_snapshot_while_state_lock_is_held(t *testing.T) {
	// Given
	store, initial, lease := newInternalTestJob(t)
	t.Cleanup(func() { require.NoError(t, lease.release()) })
	store.lockTimeout = 2 * time.Second
	releaseState := holdObservedStateLock(t, store, initial.Job.JobID)
	t.Cleanup(releaseState)
	type loadResult struct {
		snapshot Snapshot
		err      error
	}
	loaded := make(chan loadResult, 1)

	// When
	go func() {
		snapshot, err := store.Load(initial.Job.JobID)
		loaded <- loadResult{snapshot: snapshot, err: err}
	}()
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()

	// Then
	select {
	case result := <-loaded:
		require.NoError(t, result.err)
		require.Equal(t, initial, result.snapshot.State)
	case <-timer.C:
		releaseState()
		result := <-loaded
		require.NoError(t, result.err)
		t.Fatal("Store.Load waited for state.lock instead of reading the atomic state snapshot")
	}
}

func Test_Store_Load_reports_state_snapshot_close_failure(t *testing.T) {
	// Given
	store, initial, lease := newInternalTestJob(t)
	t.Cleanup(func() { require.NoError(t, lease.release()) })
	closeFailure := errors.New("injected snapshot close failure")
	store.closeJob = func(file *os.File) error {
		closeErr := file.Close()
		if file.Name() == "state.json" {
			return errors.Join(closeErr, closeFailure)
		}
		return closeErr
	}

	// When
	_, err := store.Load(initial.Job.JobID)

	// Then
	require.ErrorIs(t, err, closeFailure)
	require.ErrorContains(t, err, "close state.json")
	require.NotContains(t, err.Error(), "%!w(<nil>)")
}

//go:build linux || darwin

package runner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_reconcileRunnerState_serializes_lost_runner_observers(t *testing.T) {
	_, invocation, jobID := newLostRunnerFixture(t, lostRunnerRuntime())
	contracts, err := contract.Load()
	require.NoError(t, err)
	store, err := OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var once sync.Once
	store.afterRecoveryLock = func() {
		once.Do(func() {
			close(firstEntered)
			<-releaseFirst
		})
	}
	type reconcileResult struct {
		active bool
		err    error
	}
	results := make(chan reconcileResult, 2)
	reconcile := func() {
		active, reconcileErr := store.reconcileRunnerState(context.Background(), jobID, time.Now().Add(time.Second))
		results <- reconcileResult{active: active, err: reconcileErr}
	}
	go reconcile()
	<-firstEntered
	secondStarted := make(chan struct{})
	go func() { close(secondStarted); reconcile() }()
	<-secondStarted
	close(releaseFirst)

	for range 2 {
		result := <-results
		require.False(t, result.active)
		require.NoError(t, result.err)
	}
	snapshot, err := store.Load(jobID)
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusRunnerLost, snapshot.State.Job.Status)
}

func Test_status_creates_private_recovery_lock_for_legacy_job(t *testing.T) {
	manager, invocation, jobID := newLostRunnerFixture(t, lostRunnerRuntime())
	path := filepath.Join(invocation.WorkspacePath(), ".managed_bash", "jobs", string(jobID), recoveryLockName)
	require.NoError(t, os.Remove(path))

	result, err := manager.Status(context.Background(), StatusRequest{Invocation: invocation, JobID: jobID})

	require.NoError(t, err)
	require.Equal(t, generated.JobStatusRunnerLost, result.Job.Status)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(privateFileMode), info.Mode().Perm())
}

func Test_recovery_helpers_reject_invalid_job_id_before_lookup(t *testing.T) {
	store, _, lease := newInternalTestJob(t)
	t.Cleanup(func() { require.NoError(t, lease.release()) })
	deadline := time.Now().Add(time.Second)

	require.ErrorIs(t, store.waitForTerminalIntent(context.Background(), "../escape", deadline), ErrInvalidJobID)
	_, err := store.reconcileRunnerState(context.Background(), "../escape", deadline)
	require.ErrorIs(t, err, ErrInvalidJobID)
}

func lostRunnerRuntime() RuntimeMetadata {
	return RuntimeMetadata{
		RunnerPID: os.Getpid(), ShellPID: os.Getpid(), ProcessGroupID: os.Getpid(),
		ProcessGroupLeaderPID: os.Getpid(), ProcessBirthIdentity: "mismatched-identity",
	}
}

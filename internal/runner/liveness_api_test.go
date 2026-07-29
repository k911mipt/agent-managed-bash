//go:build linux || darwin

package runner

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_Manager_status_reconciles_runner_loss_once_and_skips_mismatched_identity_cleanup(t *testing.T) {
	// Given
	process := exec.Command("sleep", "10")
	process.SysProcAttr = newSessionAttributes()
	require.NoError(t, process.Start())
	t.Cleanup(func() {
		_ = signalProcessGroup(process.Process.Pid, unix.SIGKILL)
		_ = process.Wait()
	})
	manager, invocation, jobID := newLostRunnerFixture(t, RuntimeMetadata{
		RunnerPID: process.Process.Pid, ShellPID: process.Process.Pid,
		ProcessGroupID: process.Process.Pid, ProcessGroupLeaderPID: process.Process.Pid, ProcessBirthIdentity: "mismatched-identity",
	})

	// When
	const readers = 16
	results := make(chan generated.JobObservation, readers)
	errorsFound := make(chan error, readers)
	var group sync.WaitGroup
	for range readers {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := manager.Status(context.Background(), StatusRequest{Invocation: invocation, JobID: jobID})
			results <- result
			errorsFound <- err
		}()
	}
	group.Wait()
	close(results)
	close(errorsFound)

	// Then
	for err := range errorsFound {
		require.NoError(t, err)
	}
	for result := range results {
		require.Equal(t, generated.JobStatusRunnerLost, result.Job.Status)
		require.NotNil(t, result.ProcessResult)
		require.Contains(t, *result.ProcessResult.Diagnostic, "cleanup skipped")
	}
	require.NoError(t, unix.Kill(process.Process.Pid, 0))
}

func Test_Manager_status_never_signals_verified_process_group_for_lost_runner(t *testing.T) {
	// Given
	process := exec.Command("sleep", "10")
	process.SysProcAttr = newSessionAttributes()
	require.NoError(t, process.Start())
	t.Cleanup(func() {
		_ = signalProcessGroup(process.Process.Pid, unix.SIGKILL)
		_ = process.Wait()
	})
	identity, err := processBirthIdentity(process.Process.Pid)
	require.NoError(t, err)
	manager, invocation, jobID := newLostRunnerFixture(t, RuntimeMetadata{
		RunnerPID: process.Process.Pid, ShellPID: process.Process.Pid,
		ProcessGroupID: process.Process.Pid, ProcessGroupLeaderPID: process.Process.Pid, ProcessBirthIdentity: identity,
	})

	// When
	result, statusErr := manager.Status(context.Background(), StatusRequest{Invocation: invocation, JobID: jobID})

	// Then
	require.NoError(t, statusErr)
	require.Equal(t, generated.JobStatusRunnerLost, result.Job.Status)
	require.Contains(t, *result.ProcessResult.Diagnostic, "cleanup skipped")
	require.NoError(t, unix.Kill(process.Process.Pid, 0))
}

func Test_Manager_cancel_reconciles_lost_runner_before_control_decision(t *testing.T) {
	// Given
	manager, owner, jobID := newLostRunnerFixture(t, RuntimeMetadata{
		RunnerPID: os.Getpid(), ShellPID: os.Getpid(),
		ProcessGroupID: os.Getpid(), ProcessGroupLeaderPID: os.Getpid(), ProcessBirthIdentity: "mismatched-identity",
	})
	other := trustedInvocationInWorkspace(t, "other", owner.WorkspacePath())

	// When
	_, nonOwnerErr := manager.Cancel(context.Background(), CancelRequest{Invocation: other, JobID: jobID})
	result, ownerErr := manager.Cancel(context.Background(), CancelRequest{Invocation: owner, JobID: jobID})

	// Then
	require.ErrorIs(t, nonOwnerErr, ErrUnauthorized)
	require.NoError(t, ownerErr)
	require.Equal(t, generated.CancellationOutcomeAlreadyTerminal, result.Outcome)
	require.Equal(t, generated.JobStatusRunnerLost, result.Status)
	require.Nil(t, result.Cancellation)
}

func Test_Manager_remove_reconciles_lost_runner_without_prior_status(t *testing.T) {
	// Given
	manager, owner, jobID := newLostRunnerFixture(t, RuntimeMetadata{
		RunnerPID: os.Getpid(), ShellPID: os.Getpid(),
		ProcessGroupID: os.Getpid(), ProcessGroupLeaderPID: os.Getpid(), ProcessBirthIdentity: "mismatched-identity",
	})
	other := trustedInvocationInWorkspace(t, "other", owner.WorkspacePath())

	// When
	_, nonOwnerErr := manager.Remove(context.Background(), RemoveRequest{Invocation: other, JobID: jobID})
	result, ownerErr := manager.Remove(context.Background(), RemoveRequest{Invocation: owner, JobID: jobID})

	// Then
	require.ErrorIs(t, nonOwnerErr, ErrUnauthorized)
	require.NoError(t, ownerErr)
	require.Equal(t, generated.RemoveResult{JobID: jobID, Removed: true}, result)
}

func trustedInvocationInWorkspace(
	t *testing.T,
	sessionID generated.SessionID,
	workspace string,
) state.TrustedInvocation {
	t.Helper()
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: sessionID, WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: sessionID, WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	return invocation
}

func newLostRunnerFixture(
	t *testing.T,
	runtime RuntimeMetadata,
) (*Manager, state.TrustedInvocation, generated.JobID) {
	t.Helper()
	workspace := testWorkspace(t)
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	store, err := OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	initial := internalRunningState(workspace)
	pending, err := store.Prepare(initial, runtime)
	require.NoError(t, err)
	lease, err := pending.Commit()
	require.NoError(t, err)
	require.NoError(t, lease.Release())
	manager, err := New(Config{PollInterval: 5 * time.Millisecond})
	require.NoError(t, err)
	return manager, invocation, initial.Job.JobID
}

func newInternalTestJob(t *testing.T) (*Store, generated.PersistedJobState, *RunnerLease) {
	t.Helper()
	workspace := testWorkspace(t)
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	store, err := OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	initial := internalRunningState(workspace)
	pending, err := store.Prepare(initial, RuntimeMetadata{
		RunnerPID: os.Getpid(), ShellPID: os.Getpid(), ProcessGroupID: os.Getpid(), ProcessGroupLeaderPID: os.Getpid(), ProcessBirthIdentity: "held-lease",
	})
	require.NoError(t, err)
	lease, err := pending.Commit()
	require.NoError(t, err)
	return store, initial, lease
}

func internalRunningState(workspace string) generated.PersistedJobState {
	return generated.PersistedJobState{
		SchemaVersion: 1,
		Session: generated.SessionMetadata{
			SchemaVersion: 1, SessionID: "owner", WorkspacePath: workspace, CreatedAtUnixMs: 100,
		},
		Job: generated.JobMetadata{
			JobID: "job-lost", Status: generated.JobStatusRunning, OwnerSessionID: "owner",
			WorkspacePath: workspace, Cwd: workspace, Command: "sleep 10", CreatedAtUnixMs: 101,
			StartedAtUnixMs: 102, HardTimeoutMs: 10_000, OutputLimitBytes: 1024,
		},
		Observers: []generated.ObserverCursor{},
	}
}

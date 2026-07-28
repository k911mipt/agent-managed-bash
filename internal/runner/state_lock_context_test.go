//go:build linux || darwin

package runner

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_Manager_control_operations_honor_context_while_state_lock_is_held(t *testing.T) {
	operations := map[string]func(context.Context, *Manager, state.TrustedInvocation, generated.JobID) error{
		"status": func(ctx context.Context, manager *Manager, invocation state.TrustedInvocation, jobID generated.JobID) error {
			_, err := manager.Status(ctx, StatusRequest{Invocation: invocation, JobID: jobID})
			return err
		},
		"output": func(ctx context.Context, manager *Manager, invocation state.TrustedInvocation, jobID generated.JobID) error {
			_, err := manager.Output(ctx, OutputRequest{Invocation: invocation, JobID: jobID})
			return err
		},
		"cancel": func(ctx context.Context, manager *Manager, invocation state.TrustedInvocation, jobID generated.JobID) error {
			_, err := manager.Cancel(ctx, CancelRequest{Invocation: invocation, JobID: jobID})
			return err
		},
		"list": func(ctx context.Context, manager *Manager, invocation state.TrustedInvocation, _ generated.JobID) error {
			_, err := manager.List(ctx, ListRequest{Invocation: invocation})
			return err
		},
		"remove": func(ctx context.Context, manager *Manager, invocation state.TrustedInvocation, jobID generated.JobID) error {
			_, err := manager.Remove(ctx, RemoveRequest{Invocation: invocation, JobID: jobID})
			return err
		},
		"prepare wait": func(ctx context.Context, manager *Manager, invocation state.TrustedInvocation, jobID generated.JobID) error {
			_, err := manager.PrepareWait(ctx, WaitRequest{
				Invocation: invocation, JobID: jobID, Timeout: time.Second, IdleTimeout: time.Second,
			})
			return err
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			manager, invocation, jobID, holdLock := newStateLockFixture(t)
			release := holdLock(t)
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()

			err := operation(ctx, manager, invocation, jobID)

			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}

func Test_PreparedWait_commit_honors_context_while_state_lock_is_held(t *testing.T) {
	// Given
	manager, invocation, jobID, holdLock := newStateLockFixture(t)
	prepared, err := manager.PrepareWait(context.Background(), WaitRequest{
		Invocation: invocation, JobID: jobID, Timeout: time.Second, IdleTimeout: time.Millisecond,
	})
	require.NoError(t, err)
	release := holdLock(t)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	// When
	err = prepared.Commit(ctx)

	// Then
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func Test_Manager_prepare_wait_reads_output_only_for_delivery(t *testing.T) {
	// Given
	manager, invocation, jobID, _ := newStateLockFixture(t)
	manager.config.PollInterval = time.Millisecond
	var reads atomic.Int32
	manager.beforeOutputRead = func() { reads.Add(1) }

	// When
	prepared, err := manager.PrepareWait(context.Background(), WaitRequest{
		Invocation: invocation, JobID: jobID, Timeout: time.Second, IdleTimeout: 20 * time.Millisecond,
	})

	// Then
	require.NoError(t, err)
	require.NotNil(t, prepared)
	require.Equal(t, int32(1), reads.Load())
}

func Test_Manager_prepare_wait_returns_checkpoint_when_lock_consumes_absolute_timeout(t *testing.T) {
	manager, invocation, jobID, holdLock := newStateLockFixture(t)
	manager.config.StateLockTimeout = time.Second
	release := holdLock(t)
	started := time.Now()

	prepared, err := manager.PrepareWait(context.Background(), WaitRequest{
		Invocation: invocation, JobID: jobID, Timeout: 25 * time.Millisecond, IdleTimeout: time.Second,
	})
	elapsed := time.Since(started)
	release()

	require.NoError(t, err)
	require.NotNil(t, prepared)
	require.Less(t, elapsed, 200*time.Millisecond)
	require.Equal(t, jobID, prepared.Observation.Observation.Job.JobID)
	require.NoError(t, prepared.Commit(context.Background()))
}

func Test_Store_loadContext_stops_at_recovery_deadline_while_state_lock_is_held(t *testing.T) {
	_, invocation, jobID, holdLock := newStateLockFixture(t)
	contracts, err := contract.Load()
	require.NoError(t, err)
	store, err := OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	release := holdLock(t)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()

	_, err = store.loadContext(ctx, jobID)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 200*time.Millisecond)
}

func Test_Manager_list_skips_job_removed_after_directory_enumeration(t *testing.T) {
	// Given
	workspace := testWorkspace(t)
	executable, err := os.Executable()
	require.NoError(t, err)
	manager, err := New(Config{Executable: executable, StartupTimeout: time.Second, PollInterval: time.Millisecond})
	require.NoError(t, err)
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	job, err := manager.Start(context.Background(), StartRequest{
		Invocation: invocation, Command: "true", HardTimeout: time.Second, OutputLimitBytes: 1024,
	})
	require.NoError(t, err)
	waitForCondition(t, time.Second, func() bool {
		observation, statusErr := manager.Status(context.Background(), StatusRequest{Invocation: invocation, JobID: job.JobID})
		return statusErr == nil && observation.Job.Status != generated.JobStatusRunning
	})
	var removeErr error
	manager.afterListEntries = func() {
		manager.afterListEntries = nil
		_, removeErr = manager.Remove(context.Background(), RemoveRequest{Invocation: invocation, JobID: job.JobID})
	}

	// When
	result, listErr := manager.List(context.Background(), ListRequest{Invocation: invocation})

	// Then
	require.NoError(t, removeErr)
	require.NoError(t, listErr)
	require.Empty(t, result.Jobs)
}

func newStateLockFixture(t *testing.T) (*Manager, state.TrustedInvocation, generated.JobID, func(*testing.T) func()) {
	t.Helper()
	workspace := testWorkspace(t)
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	manager, err := New(Config{StateLockTimeout: time.Second, StateLockPoll: time.Millisecond})
	require.NoError(t, err)
	store, err := OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	request := internalStartRequest{
		JobID: "job-locked", SessionID: "owner", WorkspacePath: workspace, Cwd: workspace, Command: "true",
		CreatedAtUnixMs: 100, HardTimeoutMs: 1000, OutputLimitBytes: 1024,
		StartupTimeoutMs: 1000, TerminationGraceMs: 1000, PollIntervalMs: 10,
	}
	pending, err := store.Prepare(initialJobState(request, 101), RuntimeMetadata{
		RunnerPID: os.Getpid(), ShellPID: os.Getpid(), ProcessGroupID: os.Getpid(), ProcessGroupLeaderPID: os.Getpid(), ProcessBirthIdentity: "test",
	})
	require.NoError(t, err)
	lease, err := pending.Commit()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	hold := func(t *testing.T) func() {
		t.Helper()
		directory, openErr := openDirectoryAt(store.jobs, string(request.JobID), true)
		require.NoError(t, openErr)
		stateLock, openErr := openPrivateFileAt(directory, "state.lock", unix.O_RDWR)
		require.NoError(t, openErr)
		require.NoError(t, lockFile(stateLock, false))
		return func() { require.NoError(t, unlockFile(stateLock), stateLock.Close(), directory.Close()) }
	}
	return manager, invocation, request.JobID, hold
}

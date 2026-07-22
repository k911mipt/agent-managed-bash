//go:build linux || darwin

package runner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
)

func Test_Store_prepare_commit_loads_private_job_atomically(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	runtime := testRuntime()

	// When
	pending, err := store.Prepare(initial, runtime)
	require.NoError(t, err)
	_, statErr := os.Stat(jobPath(workspace, initial.Job.JobID))
	require.ErrorIs(t, statErr, os.ErrNotExist)
	lease, err := pending.Commit()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	snapshot, err := store.Load(initial.Job.JobID)

	// Then
	require.NoError(t, err)
	require.Equal(t, initial, snapshot.State)
	require.Equal(t, runtime, snapshot.Runtime)
	requirePrivateLayout(t, workspace, initial.Job.JobID)
}

func Test_Store_publish_state_keeps_lock_files_stable_and_rejects_overclaim(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	stateLockPath := filepath.Join(jobPath(workspace, initial.Job.JobID), "state.lock")
	runnerLockPath := filepath.Join(jobPath(workspace, initial.Job.JobID), "runner.lock")
	stateLockBefore, err := os.Stat(stateLockPath)
	require.NoError(t, err)
	runnerLockBefore, err := os.Stat(runnerLockPath)
	require.NoError(t, err)
	next := initial
	next.Observers = []generated.ObserverCursor{{SessionID: "observer", UpdatedAtUnixMs: 103}}

	// When
	require.NoError(t, store.PublishState(initial.Job.JobID, next))
	overclaim := next
	overclaim.Job.CapturedBytes = 1
	err = store.PublishState(initial.Job.JobID, overclaim)
	snapshot, loadErr := store.Load(initial.Job.JobID)
	stateLockAfter, stateStatErr := os.Stat(stateLockPath)
	runnerLockAfter, runnerStatErr := os.Stat(runnerLockPath)

	// Then
	require.ErrorIs(t, err, runner.ErrInvalidStateUpdate)
	require.NoError(t, loadErr)
	require.Equal(t, next, snapshot.State)
	require.NoError(t, stateStatErr)
	require.NoError(t, runnerStatErr)
	require.True(t, os.SameFile(stateLockBefore, stateLockAfter))
	require.True(t, os.SameFile(runnerLockBefore, runnerLockAfter))
}

func Test_Store_publish_terminal_releases_runner_lease_before_removal(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	lease := commitTestJob(t, store, initial)
	terminal := succeededState(initial)

	// When
	publishErr := store.PublishTerminal(initial.Job.JobID, terminal, lease)
	removeErr := store.RemoveTerminal(initial.Job.JobID)

	// Then
	require.NoError(t, publishErr)
	require.NoError(t, removeErr)
	_, err := os.Stat(jobPath(workspace, initial.Job.JobID))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func Test_Store_publish_state_does_not_rewrite_terminal_result(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	terminal := succeededState(initial)
	require.NoError(t, store.PublishTerminal(initial.Job.JobID, terminal, lease))
	mutated := terminal
	diagnostic := "changed"
	mutated.Result.Diagnostic = &diagnostic

	// When
	err := store.PublishState(initial.Job.JobID, mutated)

	// Then
	require.ErrorIs(t, err, runner.ErrInvalidStateUpdate)
}

func Test_Store_publish_state_rejects_terminal_transition_without_runner_lease(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })

	// When
	err := store.PublishState(initial.Job.JobID, succeededState(initial))

	// Then
	require.ErrorIs(t, err, runner.ErrInvalidStateUpdate)
}

func Test_Store_publish_terminal_rejects_lease_from_another_store(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	otherStore, otherWorkspace := newTestStore(t)
	initial := runningState(workspace, 10)
	otherInitial := runningState(otherWorkspace, 10)
	lease := commitTestJob(t, store, initial)
	otherLease := commitTestJob(t, otherStore, otherInitial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	t.Cleanup(func() { require.NoError(t, otherLease.Release()) })

	// When
	err := store.PublishTerminal(initial.Job.JobID, succeededState(initial), otherLease)

	// Then
	require.ErrorIs(t, err, runner.ErrInvalidStateUpdate)
}

func Test_PendingJob_abort_removes_unpublished_job(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	pending, err := store.Prepare(initial, testRuntime())
	require.NoError(t, err)

	// When
	err = pending.Abort()
	entries, readErr := os.ReadDir(filepath.Join(workspace, ".managed_bash", "jobs"))

	// Then
	require.NoError(t, err)
	require.NoError(t, readErr)
	require.Empty(t, entries)
}

func Test_Store_remove_rejects_running_job(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })

	// When
	err := store.RemoveTerminal(initial.Job.JobID)

	// Then
	require.ErrorIs(t, err, runner.ErrActiveJob)
	_, statErr := os.Stat(jobPath(workspace, initial.Job.JobID))
	require.NoError(t, statErr)
}

func Test_Store_prepare_rejects_state_not_bound_to_trusted_invocation(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	otherCwd := filepath.Join(workspace, "other")
	require.NoError(t, os.Mkdir(otherCwd, 0o700))
	initial := runningState(workspace, 10)
	initial.Job.Cwd = otherCwd

	// When
	pending, err := store.Prepare(initial, testRuntime())
	if pending != nil {
		t.Cleanup(func() { require.NoError(t, pending.Abort()) })
	}

	// Then
	require.ErrorIs(t, err, runner.ErrInvalidStateUpdate)
}

func Test_Store_loads_job_after_trusted_cwd_is_removed(t *testing.T) {
	// Given
	workspace := filepath.Join(t.TempDir(), "workspace")
	cwd := filepath.Join(workspace, "cwd")
	require.NoError(t, os.MkdirAll(cwd, 0o700))
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "session-1", WorkspacePath: workspace, Cwd: cwd},
		generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: cwd},
	)
	require.True(t, decision.Allowed)
	store, err := runner.OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	initial := runningState(workspace, 10)
	initial.Job.Cwd = cwd
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	require.NoError(t, os.Remove(cwd))

	// When
	snapshot, err := store.Load(initial.Job.JobID)

	// Then
	require.NoError(t, err)
	require.Equal(t, cwd, snapshot.State.Job.Cwd)
}

func newTestStore(t *testing.T) (*runner.Store, string) {
	t.Helper()
	workspace := filepath.Join(t.TempDir(), "workspace")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	store, err := runner.OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store, workspace
}

func runningState(workspace string, outputLimit int) generated.PersistedJobState {
	return generated.PersistedJobState{
		SchemaVersion: 1,
		Session: generated.SessionMetadata{
			SchemaVersion: 1, SessionID: "session-1", WorkspacePath: workspace, CreatedAtUnixMs: 100,
		},
		Job: generated.JobMetadata{
			JobID: "job-1", Status: generated.JobStatusRunning, OwnerSessionID: "session-1",
			WorkspacePath: workspace, Cwd: workspace, Command: "printf ok", CreatedAtUnixMs: 101,
			StartedAtUnixMs: 102, HardTimeoutMs: 1000, OutputLimitBytes: outputLimit,
		},
		Observers: []generated.ObserverCursor{},
	}
}

func succeededState(initial generated.PersistedJobState) generated.PersistedJobState {
	terminal := initial
	finished := generated.TimestampUnixMs(200)
	exitCode := 0
	terminal.Job.Status = generated.JobStatusSucceeded
	terminal.Job.FinishedAtUnixMs = &finished
	terminal.Result = &generated.ProcessResult{
		Status: generated.TerminalStatusSucceeded, FinishedAtUnixMs: finished,
		CapturedBytes: terminal.Job.CapturedBytes, ExitCode: &exitCode,
	}
	return terminal
}

func testRuntime() runner.RuntimeMetadata {
	return runner.RuntimeMetadata{
		RunnerPID: 100, ShellPID: 101, ProcessGroupID: 101, ProcessGroupLeaderPID: 101, ProcessBirthIdentity: "birth-101",
	}
}

func commitTestJob(t *testing.T, store *runner.Store, initial generated.PersistedJobState) *runner.RunnerLease {
	t.Helper()
	pending, err := store.Prepare(initial, testRuntime())
	require.NoError(t, err)
	lease, err := pending.Commit()
	require.NoError(t, err)
	return lease
}

func jobPath(workspace string, jobID generated.JobID) string {
	return filepath.Join(workspace, ".managed_bash", "jobs", string(jobID))
}

func requirePrivateLayout(t *testing.T, workspace string, jobID generated.JobID) {
	t.Helper()
	for _, directory := range []string{
		filepath.Join(workspace, ".managed_bash"), filepath.Join(workspace, ".managed_bash", "jobs"),
		jobPath(workspace, jobID),
	} {
		info, err := os.Stat(directory)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	}
	for _, name := range []string{"state.json", "output.log", "runtime.json", "state.lock", "runner.lock"} {
		info, err := os.Stat(filepath.Join(jobPath(workspace, jobID), name))
		require.NoError(t, err)
		require.True(t, info.Mode().IsRegular())
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

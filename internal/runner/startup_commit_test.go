//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_Manager_aborts_prepared_job_when_caller_dies_before_commit(t *testing.T) {
	// Given
	workspace := t.TempDir()
	preparedPath := filepath.Join(workspace, "prepared")
	shellPIDPath := filepath.Join(workspace, "shell-pid")
	executable, err := os.Executable()
	require.NoError(t, err)
	caller := exec.Command(executable, "-test.run=^Test_PreCommitCallerProcess$")
	caller.Env = append(os.Environ(),
		"AMB_PRECOMMIT_HELPER=1",
		"AMB_PRECOMMIT_WORKSPACE="+workspace,
		"AMB_PRECOMMIT_PREPARED="+preparedPath,
		"AMB_PRECOMMIT_SHELL_PID="+shellPIDPath,
	)
	require.NoError(t, caller.Start())
	t.Cleanup(func() {
		if caller.ProcessState == nil {
			_ = caller.Process.Kill()
			_, _ = caller.Process.Wait()
		}
	})
	waitForCondition(t, 3*time.Second, func() bool {
		_, statErr := os.Stat(preparedPath)
		return statErr == nil
	})
	waitForCondition(t, 3*time.Second, func() bool {
		_, statErr := os.Stat(shellPIDPath)
		return statErr == nil
	})
	rawPID, err := os.ReadFile(shellPIDPath)
	require.NoError(t, err)
	shellPID, err := strconv.Atoi(string(rawPID))
	require.NoError(t, err)

	// When
	require.NoError(t, caller.Process.Kill())
	require.Error(t, caller.Wait())

	// Then
	waitForCondition(t, 3*time.Second, func() bool {
		return errors.Is(unix.Kill(shellPID, 0), unix.ESRCH)
	})
	waitForCondition(t, 3*time.Second, func() bool {
		entries, readErr := os.ReadDir(filepath.Join(workspace, ".managed_bash", "jobs"))
		return readErr == nil && len(entries) == 0
	})
}

func Test_Manager_returns_committed_job_when_context_is_canceled_after_commit(t *testing.T) {
	// Given
	workspace := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	manager, err := New(Config{Executable: executable, StartupTimeout: time.Second, TerminationGrace: 50 * time.Millisecond})
	require.NoError(t, err)
	contracts, err := contract.Load()
	require.NoError(t, err)
	owner, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	ctx, cancel := context.WithCancel(context.Background())
	manager.afterCommit = cancel

	// When
	job, err := manager.Start(ctx, StartRequest{
		Invocation: owner, Command: "printf committed", HardTimeout: time.Second, OutputLimitBytes: 1024,
	})

	// Then
	require.NoError(t, err)
	require.NotEmpty(t, job.JobID)
	observation, statusErr := manager.Status(context.Background(), StatusRequest{Invocation: owner, JobID: job.JobID})
	require.NoError(t, statusErr)
	require.Equal(t, job.JobID, observation.Job.JobID)
}

func Test_PendingJob_returns_lease_when_parent_sync_fails_after_publication(t *testing.T) {
	// Given
	workspace := t.TempDir()
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
	request := internalStartRequest{
		JobID: "job-sync-failure", SessionID: "owner", WorkspacePath: workspace, Cwd: workspace,
		Command: "true", CreatedAtUnixMs: 100, HardTimeoutMs: 1000, OutputLimitBytes: 1024,
		StartupTimeoutMs: 1000, TerminationGraceMs: 1000, PollIntervalMs: 10,
	}
	initial := initialJobState(request, 101)
	pending, err := store.Prepare(initial, RuntimeMetadata{
		RunnerPID: os.Getpid(), ShellPID: os.Getpid(), ProcessGroupID: os.Getpid(), ProcessGroupLeaderPID: os.Getpid(), ProcessBirthIdentity: "test",
	})
	require.NoError(t, err)
	injected := errors.New("injected sync failure")
	store.syncJobs = func() error { return injected }

	// When
	lease, commitErr := pending.Commit()

	// Then
	require.ErrorIs(t, commitErr, injected)
	require.NotNil(t, lease)
	require.NoError(t, pending.Abort())
	snapshot, loadErr := store.Load(initial.Job.JobID)
	require.NoError(t, loadErr)
	require.Equal(t, initial.Job.JobID, snapshot.State.Job.JobID)
	require.NoError(t, lease.Release())
}

func Test_committedResult_returns_metadata_with_durability_error(t *testing.T) {
	job := generated.JobMetadata{JobID: "job-durability"}

	result, err := committedResult(internalCommitted{Job: job, DurabilityError: "sync jobs: injected"})

	require.Equal(t, job, result)
	var durabilityErr *CommitDurabilityError
	require.ErrorAs(t, err, &durabilityErr)
	require.Equal(t, job.JobID, durabilityErr.JobID)
}

func Test_recoverCommitted_returns_visible_metadata_with_uncertain_durability(t *testing.T) {
	workspace := t.TempDir()
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "owner", WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	capability, err := openWorkspaceDirectory(workspace)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, capability.Close()) })
	store, err := OpenStoreAt(invocation, contracts, capability)
	require.NoError(t, err)
	request := internalStartRequest{
		JobID: "job-recovered", SessionID: "owner", WorkspacePath: workspace, Cwd: workspace,
		Command: "true", CreatedAtUnixMs: 100, HardTimeoutMs: 1000, OutputLimitBytes: 1024,
		StartupTimeoutMs: 1000, TerminationGraceMs: 1000, PollIntervalMs: 5,
	}
	pending, err := store.prepare(initialJobState(request, 101), RuntimeMetadata{
		RunnerPID: os.Getpid(), ShellPID: os.Getpid(), ProcessGroupID: os.Getpid(),
		ProcessGroupLeaderPID: os.Getpid(), ProcessBirthIdentity: "test",
	})
	require.NoError(t, err)
	lease, err := pending.commit()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lease.release(), store.Close()) })
	manager, err := New(Config{StartupTimeout: 50 * time.Millisecond, PollInterval: time.Millisecond})
	require.NoError(t, err)

	job, recoverErr := (startupHandshake{
		manager: manager, request: StartRequest{Invocation: invocation}, jobID: request.JobID, workspace: capability,
	}).recoverCommitted(errors.New("acknowledgement lost"))

	require.Equal(t, request.JobID, job.JobID)
	var durabilityErr *CommitDurabilityError
	require.ErrorAs(t, recoverErr, &durabilityErr)
	require.ErrorIs(t, recoverErr, ErrCommitDurabilityUnknown)
}

func Test_createPrivateDirectory_removes_staging_directory_when_open_fails(t *testing.T) {
	// Given
	workspace := t.TempDir()
	parent, err := openWorkspaceDirectory(workspace)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, parent.Close()) })
	injected := errors.New("injected open failure")

	// When
	directory, createErr := createPrivateDirectoryWith(parent, "staging", directoryCreateOps{
		open: func(*os.File, string) (*os.File, error) { return nil, injected },
		sync: func(parent *os.File) error { return parent.Sync() },
	})

	// Then
	require.Nil(t, directory)
	require.ErrorIs(t, createErr, injected)
	_, statErr := os.Stat(filepath.Join(workspace, "staging"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func Test_createPrivateDirectory_removes_staging_directory_when_sync_fails(t *testing.T) {
	// Given
	workspace := t.TempDir()
	parent, err := openWorkspaceDirectory(workspace)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, parent.Close()) })
	injected := errors.New("injected sync failure")

	// When
	directory, createErr := createPrivateDirectoryWith(parent, "staging", directoryCreateOps{
		open: func(parent *os.File, name string) (*os.File, error) { return openDirectoryAt(parent, name, true) },
		sync: func(*os.File) error { return injected },
	})

	// Then
	require.Nil(t, directory)
	require.ErrorIs(t, createErr, injected)
	_, statErr := os.Stat(filepath.Join(workspace, "staging"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func Test_PreCommitCallerProcess(t *testing.T) {
	if os.Getenv("AMB_PRECOMMIT_HELPER") != "1" {
		return
	}
	workspace := os.Getenv("AMB_PRECOMMIT_WORKSPACE")
	preparedPath := os.Getenv("AMB_PRECOMMIT_PREPARED")
	shellPIDPath := os.Getenv("AMB_PRECOMMIT_SHELL_PID")
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "session-precommit", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "session-precommit", WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	executable, err := os.Executable()
	require.NoError(t, err)
	manager, err := New(Config{Executable: executable, StartupTimeout: 3 * time.Second, TerminationGrace: 40 * time.Millisecond})
	require.NoError(t, err)
	manager.beforeCommit = func() {
		require.NoError(t, os.WriteFile(preparedPath, []byte("prepared"), 0o600))
		select {}
	}
	command := `printf '%s' "$$" >` + strconv.Quote(shellPIDPath) + `; trap '' TERM; exec sleep 10`
	_, err = manager.Start(context.Background(), StartRequest{
		Invocation: invocation, Command: command, HardTimeout: 10 * time.Second, OutputLimitBytes: 1024,
	})
	require.NoError(t, err)
}

func waitForCondition(t *testing.T, maximumWait time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(maximumWait)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-deadline.C:
			t.Fatal("condition did not become true")
		case <-ticker.C:
		}
	}
}

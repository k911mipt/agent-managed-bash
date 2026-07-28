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

func Test_OpenStore_rejects_symlinked_managed_directory(t *testing.T) {
	// Given
	workspace := runner.NewTestWorkspace(t)
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.Mkdir(outside, 0o700))
	require.NoError(t, os.Symlink(outside, filepath.Join(workspace, ".managed_bash")))
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)

	// When
	store, err := runner.OpenStore(invocation, contracts)

	// Then
	require.Nil(t, store)
	require.ErrorIs(t, err, runner.ErrUnsafeFilesystem)
}

func Test_Store_load_rejects_symlinked_state_file(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	statePath := filepath.Join(jobPath(workspace, initial.Job.JobID), "state.json")
	external := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(external, []byte("external"), 0o600))
	require.NoError(t, os.Remove(statePath))
	require.NoError(t, os.Symlink(external, statePath))

	// When
	_, err := store.Load(initial.Job.JobID)
	content, readErr := os.ReadFile(external)

	// Then
	require.ErrorIs(t, err, runner.ErrUnsafeFilesystem)
	require.NoError(t, readErr)
	require.Equal(t, []byte("external"), content)
}

func Test_Store_append_rejects_symlinked_output_file(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	outputPath := filepath.Join(jobPath(workspace, initial.Job.JobID), "output.log")
	external := filepath.Join(t.TempDir(), "output.log")
	require.NoError(t, os.WriteFile(external, []byte("external"), 0o600))
	require.NoError(t, os.Remove(outputPath))
	require.NoError(t, os.Symlink(external, outputPath))

	// When
	_, err := store.AppendOutput(initial.Job.JobID, []byte("overwrite"))
	content, readErr := os.ReadFile(external)

	// Then
	require.ErrorIs(t, err, runner.ErrUnsafeFilesystem)
	require.NoError(t, readErr)
	require.Equal(t, []byte("external"), content)
}

func Test_Store_rejects_invalid_job_identifier_before_lookup(t *testing.T) {
	// Given
	store, _ := newTestStore(t)

	// When
	_, err := store.Load("../escape")

	// Then
	require.ErrorIs(t, err, runner.ErrInvalidJobID)
}

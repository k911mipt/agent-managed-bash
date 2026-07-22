//go:build linux || darwin

package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
)

func Test_OpenStoreAt_uses_workspace_descriptor_after_path_replacement(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	moved := filepath.Join(base, "moved")
	replacement := filepath.Join(base, "replacement")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	require.NoError(t, os.Mkdir(replacement, 0o700))
	invocation := bindTestInvocation(t, workspace, workspace)
	capability, err := openWorkspaceDirectory(workspace)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, capability.Close()) })
	require.NoError(t, os.Rename(workspace, moved))
	require.NoError(t, os.Mkdir(workspace, 0o700))
	contracts, err := contract.Load()
	require.NoError(t, err)

	store, err := OpenStoreAt(invocation, contracts, capability)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	_, movedErr := os.Stat(filepath.Join(moved, ".managed_bash", "jobs"))
	_, replacementErr := os.Stat(filepath.Join(workspace, ".managed_bash"))
	require.NoError(t, movedErr)
	require.ErrorIs(t, replacementErr, os.ErrNotExist)
}

func Test_Manager_executes_from_cwd_descriptor_after_path_replacement(t *testing.T) {
	workspace := t.TempDir()
	cwd := filepath.Join(workspace, "cwd")
	moved := filepath.Join(workspace, "moved")
	require.NoError(t, os.Mkdir(cwd, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "marker"), []byte("original"), 0o600))
	invocation := bindTestInvocation(t, workspace, cwd)
	executable, err := os.Executable()
	require.NoError(t, err)
	manager, err := New(Config{Executable: executable, StartupTimeout: time.Second, TerminationGrace: 20 * time.Millisecond})
	require.NoError(t, err)
	manager.afterCapabilitiesOpened = func() {
		require.NoError(t, os.Rename(cwd, moved))
		require.NoError(t, os.Mkdir(cwd, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(cwd, "marker"), []byte("replacement"), 0o600))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	job, err := manager.Start(ctx, StartRequest{
		Invocation: invocation, Command: "cat marker", HardTimeout: time.Second, OutputLimitBytes: 1024,
	})
	require.NoError(t, err)
	contracts, err := contract.Load()
	require.NoError(t, err)
	store, err := OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	waitForCondition(t, 2*time.Second, func() bool {
		snapshot, loadErr := store.Load(job.JobID)
		return loadErr == nil && snapshot.State.Job.Status != generated.JobStatusRunning
	})
	output, err := store.ReadOutput(job.JobID)
	require.NoError(t, err)
	require.Equal(t, "original", string(output))
}

func Test_Manager_publishes_to_workspace_descriptor_after_path_replacement(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	moved := filepath.Join(base, "moved")
	replacement := filepath.Join(base, "replacement")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	require.NoError(t, os.Mkdir(replacement, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "marker"), []byte("original"), 0o600))
	invocation := bindTestInvocation(t, workspace, workspace)
	capability, err := openWorkspaceDirectory(workspace)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, capability.Close()) })
	executable, err := os.Executable()
	require.NoError(t, err)
	manager, err := New(Config{Executable: executable, StartupTimeout: time.Second, TerminationGrace: 20 * time.Millisecond})
	require.NoError(t, err)
	manager.afterCapabilitiesOpened = func() {
		require.NoError(t, os.Rename(workspace, moved))
		require.NoError(t, os.WriteFile(filepath.Join(replacement, "marker"), []byte("replacement"), 0o600))
		require.NoError(t, os.Symlink(replacement, workspace))
	}

	job, err := manager.Start(context.Background(), StartRequest{
		Invocation: invocation, Command: "cat marker", HardTimeout: time.Second, OutputLimitBytes: 1024,
	})
	require.NoError(t, err)
	contracts, err := contract.Load()
	require.NoError(t, err)
	store, err := OpenStoreAt(invocation, contracts, capability)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	waitForCondition(t, 2*time.Second, func() bool {
		snapshot, loadErr := store.Load(job.JobID)
		return loadErr == nil && snapshot.State.Job.Status != generated.JobStatusRunning
	})
	output, err := store.ReadOutput(job.JobID)
	require.NoError(t, err)
	require.Equal(t, "original", string(output))
	_, replacementErr := os.Stat(filepath.Join(replacement, ".managed_bash"))
	require.ErrorIs(t, replacementErr, os.ErrNotExist)
}

func bindTestInvocation(t *testing.T, workspace string, cwd string) state.TrustedInvocation {
	t.Helper()
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "capability", WorkspacePath: workspace, Cwd: cwd},
		generated.TrustedContext{SessionID: "capability", WorkspacePath: workspace, Cwd: cwd},
	)
	require.True(t, decision.Allowed)
	return invocation
}

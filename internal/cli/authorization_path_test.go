package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Application_allows_non_owner_read_and_rejects_non_owner_mutation(t *testing.T) {
	// Given
	harness := newLifecycleHarness(t)
	jobID := harness.runJob(t, "sleep 30")
	observer := testClient{application: harness.client.application, workspace: harness.client.workspace, session: "session-2"}
	observerContext := generated.TrustedContext{
		SessionID: "session-2", WorkspacePath: harness.client.workspace, Cwd: harness.client.workspace,
	}
	status := generated.StatusRequest{
		Action: "status", Context: observerContext, Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1,
	}
	cancel := generated.CancelRequest{
		Action: "cancel", Context: observerContext, Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1,
	}

	// When: same-workspace read
	readExit, readStdout, readStderr := observer.execute(t, "status", marshalRequest(t, status))

	// Then: read succeeds
	require.Equal(t, 0, readExit)
	require.Empty(t, readStderr)
	var statusResponse generated.StatusResponse
	require.NoError(t, json.Unmarshal([]byte(readStdout), &statusResponse))
	require.Equal(t, jobID, statusResponse.Result.Job.JobID)

	// When: non-owner mutation
	mutationExit, mutationStdout, mutationStderr := observer.execute(t, "cancel", marshalRequest(t, cancel))

	// Then: mutation is unauthorized
	require.Equal(t, 3, mutationExit)
	require.Contains(t, mutationStdout, `"code":"unauthorized"`)
	require.Contains(t, mutationStderr, "code=unauthorized")

	ownerCancel := generated.CancelRequest{
		Action: "cancel", Context: harness.context, Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1,
	}
	exitCode, _, _ := harness.client.execute(t, "cancel", marshalRequest(t, ownerCancel))
	require.Equal(t, 0, exitCode)
	harness.waitForTerminal(t, jobID, generated.JobStatusCancelled)
}

func Test_Application_rejects_symlink_workspace_and_outside_cwd(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	workspaceLink := filepath.Join(base, "workspace-link")
	require.NoError(t, os.Symlink(workspace, workspaceLink))
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.Mkdir(outside, 0o700))
	previous, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })

	tests := []struct {
		name     string
		hostRoot string
		cwd      string
		asserted generated.TrustedContext
	}{
		{
			name: "symlink workspace", hostRoot: workspaceLink, cwd: workspace,
			asserted: generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspaceLink, Cwd: workspace},
		},
		{
			name: "outside cwd", hostRoot: workspace, cwd: outside,
			asserted: generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: outside},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			require.NoError(t, os.Chdir(testCase.cwd))
			client := testClient{application: application, workspace: testCase.hostRoot, session: "session-1"}
			request := generated.ListRequest{Action: "list", Context: testCase.asserted, SchemaVersion: 1}

			// When
			exitCode, stdout, stderr := client.execute(t, "list", marshalRequest(t, request))

			// Then
			require.Equal(t, 3, exitCode)
			require.Contains(t, stdout, `"code":"unauthorized"`)
			require.Contains(t, stderr, "code=unauthorized")
		})
	}
}

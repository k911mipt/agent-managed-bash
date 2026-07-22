package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_Application_run_returns_runner_unavailable_for_missing_executable(t *testing.T) {
	// Given
	workspace := t.TempDir()
	useWorkingDirectory(t, workspace)
	application, err := New(Config{
		BinaryVersion: "dev",
		Runner:        runner.Config{Executable: filepath.Join(workspace, "missing-managed-bash")},
	})
	require.NoError(t, err)
	request := generated.RunRequest{
		Action:        "run",
		Context:       generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace},
		Payload:       generated.RunPayload{Command: "printf unreachable"},
		SchemaVersion: 1,
	}

	// When
	exitCode, stdout, stderr := (testClient{
		application: application, workspace: workspace, session: "session-1",
	}).execute(t, "run", marshalRequest(t, request))

	// Then
	require.Equal(t, 5, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"action":"run","error":{"code":"runner_unavailable","message":"runner is unavailable"}}`, stdout)
	require.Contains(t, stderr, "action=run code=runner_unavailable")
	require.NotContains(t, stderr, fmt.Sprint(request.Payload.Command))
}

func Test_Application_run_returns_runner_unavailable_for_incompatible_executable(t *testing.T) {
	// Given
	workspace := t.TempDir()
	useWorkingDirectory(t, workspace)
	executable := filepath.Join(workspace, "incompatible-managed-bash")
	require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\nprintf 'OLD!000000' >&3\n"), 0o700))
	application, err := New(Config{
		BinaryVersion: "dev",
		Runner:        runner.Config{Executable: executable},
	})
	require.NoError(t, err)
	request := generated.RunRequest{
		Action:        "run",
		Context:       generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace},
		Payload:       generated.RunPayload{Command: "printf unreachable"},
		SchemaVersion: 1,
	}

	// When
	exitCode, stdout, stderr := (testClient{
		application: application, workspace: workspace, session: "session-1",
	}).execute(t, "run", marshalRequest(t, request))

	// Then
	require.Equal(t, 5, exitCode)
	require.Contains(t, stdout, `"code":"runner_unavailable"`)
	require.Contains(t, stderr, "code=runner_unavailable")
}

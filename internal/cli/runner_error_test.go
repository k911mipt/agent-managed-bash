package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_Application_run_observation_failure_returns_recoverable_job(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	job := generated.JobMetadata{
		JobID: "job-1", Status: generated.JobStatusRunning, OwnerSessionID: "session-1",
		WorkspacePath: "/workspace", Cwd: "/workspace", Command: "sleep 30",
		CreatedAtUnixMs: 1000, StartedAtUnixMs: 1001, CapturedBytes: 0,
		HardTimeoutMs: 60000, OutputLimitBytes: 104857600,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	exitCode := application.writeFailure(Streams{Stdout: &stdout, Stderr: &stderr}, runObservationFailure(job, runner.ErrCorruptState))

	// Then
	require.Equal(t, 5, exitCode)
	require.JSONEq(t, `{
		"schema_version": 1, "ok": false, "action": "run",
		"error": {"code": "corrupt_state", "message": "persisted state is corrupt"},
		"job": {
			"job_id": "job-1", "status": "running", "owner_session_id": "session-1",
			"workspace_path": "/workspace", "cwd": "/workspace", "command": "sleep 30",
			"created_at_unix_ms": 1000, "started_at_unix_ms": 1001, "captured_bytes": 0,
			"hard_timeout_ms": 60000, "output_limit_bytes": 104857600
		}
	}`, stdout.String())
}

func Test_Application_run_returns_runner_unavailable_for_missing_executable(t *testing.T) {
	// Given
	workspace := testWorkspace(t)
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
	workspace := testWorkspace(t)
	useWorkingDirectory(t, workspace)
	executable := filepath.Join(workspace, "incompatible-managed-bash")
	require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o700))
	application, err := New(Config{
		BinaryVersion: "dev",
		Runner:        runner.Config{Executable: executable},
	})
	require.NoError(t, err)
	request := generated.RunRequest{
		Action:        "run",
		Context:       generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace},
		Payload:       generated.RunPayload{Command: strings.Repeat("x", 65_536)},
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

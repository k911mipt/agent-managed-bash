package cli

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

type lifecycleHarness struct {
	client  testClient
	context generated.TrustedContext
}

func newLifecycleHarness(t *testing.T) lifecycleHarness {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	application, err := New(Config{
		BinaryVersion: "dev",
		Runner: runner.Config{
			Executable: executable, StartupTimeout: 3 * time.Second,
			TerminationGrace: 40 * time.Millisecond, PollInterval: 5 * time.Millisecond,
			StateLockTimeout: time.Second, StateLockPoll: 5 * time.Millisecond,
		},
	})
	require.NoError(t, err)
	workspace := testWorkspace(t)
	useWorkingDirectory(t, workspace)
	context := generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace}
	return lifecycleHarness{
		client:  testClient{application: application, workspace: workspace, session: "session-1"},
		context: context,
	}
}

func (harness lifecycleHarness) startJob(t *testing.T, command string) generated.JobID {
	t.Helper()
	request := generated.StartRequest{
		Action: "start", Context: harness.context, Payload: generated.StartPayload{Command: command}, SchemaVersion: 1,
	}
	exitCode, stdout, stderr := harness.client.execute(t, "start", marshalRequest(t, request))
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var response generated.StartResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &response))
	require.Equal(t, generated.JobStatusRunning, response.Result.Status)
	return response.Result.JobID
}

func (harness lifecycleHarness) waitForTerminal(t *testing.T, jobID generated.JobID, expected generated.JobStatus) {
	t.Helper()
	timeout := generated.TimeoutMs(5000)
	idle := generated.TimeoutMs(5000)
	request := generated.WaitRequest{
		Action: "wait", Context: harness.context,
		Payload:       generated.WaitPayload{JobID: jobID, TimeoutMs: &timeout, IdleTimeoutMs: &idle},
		SchemaVersion: 1,
	}
	exitCode, stdout, stderr := harness.client.execute(t, "wait", marshalRequest(t, request))
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var response generated.WaitResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &response))
	require.Equal(t, expected, response.Result.Observation.Job.Status)
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Application_failed_wait_stdout_preserves_observer_cursor(t *testing.T) {
	// Given
	harness := newLifecycleHarness(t)
	jobID := harness.runJob(t, "printf repeat")
	timeout := generated.TimeoutMs(5000)
	idle := generated.TimeoutMs(5000)
	request := generated.WaitRequest{
		Action: "wait", Context: harness.context,
		Payload:       generated.WaitPayload{JobID: jobID, TimeoutMs: &timeout, IdleTimeoutMs: &idle},
		SchemaVersion: 1,
	}
	raw := marshalRequest(t, request)
	t.Setenv(testHostSessionEnvironment, harness.client.session)
	t.Setenv(testHostWorkspaceEnvironment, harness.client.workspace)
	writer := &recordingErrorWriter{err: errors.New("stdout unavailable")}
	var stderr bytes.Buffer

	// When: delivery fails
	exitCode := harness.client.application.Execute(context.Background(), []string{"wait"}, Streams{
		Stdin: bytes.NewReader(raw), Stdout: writer, Stderr: &stderr,
	})

	// Then: no cursor commit is reported as I/O failure
	require.Equal(t, 5, exitCode)
	require.Equal(t, 1, writer.calls)
	require.Contains(t, stderr.String(), "code=io_failure")

	// When: wait is retried
	exitCode, stdout, stderrText := harness.client.execute(t, "wait", raw)

	// Then: output is delivered again from the uncommitted cursor
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderrText)
	var response generated.WaitResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &response))
	require.Equal(t, "repeat", response.Result.Output.Text)
}

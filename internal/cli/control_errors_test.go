package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Application_rejects_active_remove_then_cancels_job(t *testing.T) {
	// Given
	harness := newLifecycleHarness(t)
	jobID := harness.runJob(t, "sleep 30")
	remove := generated.RemoveRequest{
		Action: "remove", Context: harness.context, Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1,
	}

	// When: remove active job
	exitCode, stdout, stderr := harness.client.execute(t, "remove", marshalRequest(t, remove))

	// Then: active conflict
	require.Equal(t, 4, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"action":"remove","error":{"code":"active_job","message":"job is active"}}`, stdout)
	require.Contains(t, stderr, "code=active_job")

	// When: cancel and wait
	cancel := generated.CancelRequest{
		Action: "cancel", Context: harness.context, Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1,
	}
	exitCode, stdout, stderr = harness.client.execute(t, "cancel", marshalRequest(t, cancel))

	// Then: cancellation requested
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var cancelResponse generated.CancelResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &cancelResponse))
	require.Equal(t, generated.CancellationOutcomeRequested, cancelResponse.Result.Outcome)
	harness.waitForTerminal(t, jobID, generated.JobStatusCancelled)
}

func Test_Application_status_returns_corrupt_state_for_invalid_persisted_job(t *testing.T) {
	// Given
	harness := newLifecycleHarness(t)
	jobID := harness.runJob(t, "printf ok")
	harness.waitForTerminal(t, jobID, generated.JobStatusSucceeded)
	statePath := filepath.Join(harness.client.workspace, ".managed_bash", "jobs", string(jobID), "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{}`), 0o600))
	status := generated.StatusRequest{
		Action: "status", Context: harness.context, Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1,
	}

	// When
	exitCode, stdout, stderr := harness.client.execute(t, "status", marshalRequest(t, status))

	// Then
	require.Equal(t, 5, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"action":"status","error":{"code":"corrupt_state","message":"persisted state is corrupt"}}`, stdout)
	require.Contains(t, stderr, "code=corrupt_state")
}

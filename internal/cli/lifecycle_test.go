package cli

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Application_executes_complete_managed_job_lifecycle(t *testing.T) {
	// Given
	harness := newLifecycleHarness(t)
	timeout := generated.TimeoutMs(5000)
	idle := generated.TimeoutMs(5000)
	run := generated.RunRequest{
		Action: string(generated.ActionRun), Context: harness.context,
		Payload: generated.RunPayload{
			Command: `test -z "${MANAGED_BASH_HOST_SESSION_ID:-}" && test -z "${MANAGED_BASH_HOST_WORKSPACE_PATH:-}" && printf hello`,
			TimeoutMs: &timeout, IdleTimeoutMs: &idle,
		},
		SchemaVersion: 1,
	}

	// When: run
	exitCode, stdout, stderr := harness.client.execute(t, "run", marshalRequest(t, run))

	// Then: run response
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var runResponse generated.RunResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &runResponse))
	require.Equal(t, generated.ObservationReasonTerminal, runResponse.Result.Reason)
	require.Equal(t, generated.JobStatusSucceeded, runResponse.Result.Observation.Job.Status)
	require.Equal(t, "hello", runResponse.Result.Output.Text)
	jobID := runResponse.Result.Observation.Job.JobID

	// When: wait continues from the cursor delivered by run
	wait := generated.WaitRequest{
		Action: string(generated.ActionWait), Context: harness.context,
		Payload:       generated.WaitPayload{JobID: jobID, TimeoutMs: &timeout, IdleTimeoutMs: &idle},
		SchemaVersion: 1,
	}
	exitCode, stdout, stderr = harness.client.execute(t, "wait", marshalRequest(t, wait))

	// Then: terminal output is not resent
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var waitResponse generated.WaitResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &waitResponse))
	require.Equal(t, generated.ObservationReasonTerminal, waitResponse.Result.Reason)
	require.Equal(t, generated.JobStatusSucceeded, waitResponse.Result.Observation.Job.Status)
	require.Empty(t, waitResponse.Result.Output.Text)

	harness.assertOutputBoundaries(t, jobID)
	harness.assertStatusOutputListAndCancel(t, jobID)

	// When: remove
	remove := generated.RemoveRequest{
		Action: string(generated.ActionRemove), Context: harness.context,
		Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1,
	}
	exitCode, stdout, stderr = harness.client.execute(t, "remove", marshalRequest(t, remove))

	// Then: removed
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var removeResponse generated.RemoveResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &removeResponse))
	require.Equal(t, generated.RemoveResult{JobID: jobID, Removed: true}, removeResponse.Result)
}

func Test_Application_run_returns_nonzero_as_successful_terminal_observation(t *testing.T) {
	// Given
	harness := newLifecycleHarness(t)
	timeout := generated.TimeoutMs(5000)
	idle := generated.TimeoutMs(5000)
	request := generated.RunRequest{
		Action: "run", Context: harness.context,
		Payload: generated.RunPayload{Command: "exit 7", TimeoutMs: &timeout, IdleTimeoutMs: &idle},
		SchemaVersion: 1,
	}

	// When
	exitCode, stdout, stderr := harness.client.execute(t, "run", marshalRequest(t, request))

	// Then
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var response generated.RunResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &response))
	require.Equal(t, generated.ObservationReasonTerminal, response.Result.Reason)
	require.Equal(t, generated.JobStatusNonzeroExit, response.Result.Observation.Job.Status)
	require.NotNil(t, response.Result.Observation.ProcessResult)
	require.Equal(t, 7, *response.Result.Observation.ProcessResult.ExitCode)
}

func Test_Application_run_returns_output_idle_checkpoint_for_silent_job(t *testing.T) {
	// Given
	harness := newLifecycleHarness(t)
	timeout := generated.TimeoutMs(500)
	idle := generated.TimeoutMs(20)
	request := generated.RunRequest{
		Action: "run", Context: harness.context,
		Payload: generated.RunPayload{Command: "sleep 30", TimeoutMs: &timeout, IdleTimeoutMs: &idle},
		SchemaVersion: 1,
	}

	// When
	exitCode, stdout, stderr := harness.client.execute(t, "run", marshalRequest(t, request))

	// Then
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var response generated.RunResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &response))
	require.Equal(t, generated.ObservationReasonOutputIdle, response.Result.Reason)
	require.Equal(t, generated.JobStatusRunning, response.Result.Observation.Job.Status)
	cancelJob(t, harness, response.Result.Observation.Job.JobID)
}

func Test_Application_run_returns_observation_timeout_for_active_job(t *testing.T) {
	// Given
	harness := newLifecycleHarness(t)
	timeout := generated.TimeoutMs(500)
	idle := generated.TimeoutMs(5000)
	request := generated.RunRequest{
		Action: "run", Context: harness.context,
		Payload: generated.RunPayload{
			Command: "while :; do printf x; sleep 0.01; done", TimeoutMs: &timeout, IdleTimeoutMs: &idle,
		},
		SchemaVersion: 1,
	}

	// When
	exitCode, stdout, stderr := harness.client.execute(t, "run", marshalRequest(t, request))

	// Then
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var response generated.RunResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &response))
	require.Equal(t, generated.ObservationReasonObservationTimeout, response.Result.Reason)
	require.Equal(t, generated.JobStatusRunning, response.Result.Observation.Job.Status)
	require.NotEmpty(t, response.Result.Output.Text)
	cancelJob(t, harness, response.Result.Observation.Job.JobID)
}

func cancelJob(t *testing.T, harness lifecycleHarness, jobID generated.JobID) {
	t.Helper()
	request := generated.CancelRequest{
		Action: "cancel", Context: harness.context,
		Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1,
	}
	exitCode, _, _ := harness.client.execute(t, "cancel", marshalRequest(t, request))
	require.Equal(t, 0, exitCode)
}

func (harness lifecycleHarness) assertOutputBoundaries(t *testing.T, jobID generated.JobID) {
	t.Helper()
	start := generated.ByteCursor(1)
	end := generated.ByteCursor(4)
	ranged := generated.OutputRequest{
		Action: "output", Context: harness.context,
		Payload:       generated.OutputPayload{JobID: jobID, StartCursorBytes: &start, EndCursorBytes: &end},
		SchemaVersion: 1,
	}
	exitCode, stdout, stderr := harness.client.execute(t, "output", marshalRequest(t, ranged))
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr)
	var rangedResponse generated.OutputResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &rangedResponse))
	require.Equal(t, "ell", rangedResponse.Result.Output.Text)

	invalidStart := generated.ByteCursor(4)
	invalidEnd := generated.ByteCursor(2)
	invalidRange := generated.OutputRequest{
		Action: "output", Context: harness.context,
		Payload:       generated.OutputPayload{JobID: jobID, StartCursorBytes: &invalidStart, EndCursorBytes: &invalidEnd},
		SchemaVersion: 1,
	}
	exitCode, stdout, _ = harness.client.execute(t, "output", marshalRequest(t, invalidRange))
	require.Equal(t, 2, exitCode)
	require.Contains(t, stdout, `"code":"invalid_range"`)

	invalidCursor := generated.ByteCursor(6)
	wait := generated.WaitRequest{
		Action: "wait", Context: harness.context,
		Payload:       generated.WaitPayload{JobID: jobID, CursorBytes: &invalidCursor},
		SchemaVersion: 1,
	}
	exitCode, stdout, _ = harness.client.execute(t, "wait", marshalRequest(t, wait))
	require.Equal(t, 2, exitCode)
	require.Contains(t, stdout, `"code":"invalid_cursor"`)

	missing := generated.StatusRequest{
		Action: "status", Context: harness.context,
		Payload: generated.JobReference{JobID: "job-missing"}, SchemaVersion: 1,
	}
	exitCode, stdout, _ = harness.client.execute(t, "status", marshalRequest(t, missing))
	require.Equal(t, 3, exitCode)
	require.Contains(t, stdout, `"code":"job_not_found"`)
}

func (harness lifecycleHarness) assertStatusOutputListAndCancel(t *testing.T, jobID generated.JobID) {
	t.Helper()
	requests := []struct {
		action  string
		request any
		assert  func(*testing.T, string)
	}{
		{
			action:  "status",
			request: generated.StatusRequest{Action: "status", Context: harness.context, Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1},
			assert: func(t *testing.T, raw string) {
				var response generated.StatusResponse
				require.NoError(t, json.Unmarshal([]byte(raw), &response))
				require.Equal(t, generated.JobStatusSucceeded, response.Result.Job.Status)
			},
		},
		{
			action:  "output",
			request: generated.OutputRequest{Action: "output", Context: harness.context, Payload: generated.OutputPayload{JobID: jobID}, SchemaVersion: 1},
			assert: func(t *testing.T, raw string) {
				var response generated.OutputResponse
				require.NoError(t, json.Unmarshal([]byte(raw), &response))
				require.Equal(t, "hello", response.Result.Output.Text)
			},
		},
		{
			action:  "list",
			request: generated.ListRequest{Action: "list", Context: harness.context, SchemaVersion: 1},
			assert: func(t *testing.T, raw string) {
				var response generated.ListResponse
				require.NoError(t, json.Unmarshal([]byte(raw), &response))
				require.Len(t, response.Result.Jobs, 1)
				require.Equal(t, jobID, response.Result.Jobs[0].JobID)
			},
		},
		{
			action:  "cancel",
			request: generated.CancelRequest{Action: "cancel", Context: harness.context, Payload: generated.JobReference{JobID: jobID}, SchemaVersion: 1},
			assert: func(t *testing.T, raw string) {
				var response generated.CancelResponse
				require.NoError(t, json.Unmarshal([]byte(raw), &response))
				require.Equal(t, generated.CancellationOutcomeAlreadyTerminal, response.Result.Outcome)
			},
		},
	}
	for _, request := range requests {
		t.Run(fmt.Sprintf("%s response", request.action), func(t *testing.T) {
			exitCode, stdout, stderr := harness.client.execute(t, request.action, marshalRequest(t, request.request))
			require.Equal(t, 0, exitCode)
			require.Empty(t, stderr)
			request.assert(t, stdout)
		})
	}
}

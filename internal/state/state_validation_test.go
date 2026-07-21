package state

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_ValidatePersistedState_accepts_consistent_running_and_terminal_states(t *testing.T) {
	policy := loadTestPolicy(t)
	running := validRunningState()
	terminal := validSucceededState()

	require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, policy.ValidatePersistedState(running))
	require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, policy.ValidatePersistedState(terminal))
}

func Test_ValidatePersistedState_matches_semantic_fixtures(t *testing.T) {
	policy := loadTestPolicy(t)
	for _, testCase := range loadPolicyCases(t).State {
		t.Run(testCase.Name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "v1", "policy", "state", testCase.Path))
			require.NoError(t, err)
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			var persisted generated.PersistedJobState
			require.NoError(t, decoder.Decode(&persisted))
			_, err = decoder.Token()
			require.ErrorIs(t, err, io.EOF)

			decision := policy.ValidatePersistedState(persisted)

			require.Equal(t, Decision{Allowed: testCase.Allowed, Code: testCase.Code}, decision)
		})
	}
}

func Test_ValidatePersistedState_rejects_cross_object_corruption(t *testing.T) {
	policy := loadTestPolicy(t)
	tests := []struct {
		name   string
		mutate func(state *generated.PersistedJobState)
	}{
		{
			name: "running state with result",
			mutate: func(state *generated.PersistedJobState) {
				result := validSucceededState().Result
				state.Result = result
			},
		},
		{
			name: "terminal state without result",
			mutate: func(state *generated.PersistedJobState) {
				state.Result = nil
			},
		},
		{
			name: "job and result status mismatch",
			mutate: func(state *generated.PersistedJobState) {
				state.Result.Status = generated.TerminalStatusRunnerLost
			},
		},
		{
			name: "job and result captured bytes mismatch",
			mutate: func(state *generated.PersistedJobState) {
				state.Result.CapturedBytes = 1
			},
		},
		{
			name: "observer beyond capture",
			mutate: func(state *generated.PersistedJobState) {
				state.Observers = []generated.ObserverCursor{{CursorBytes: 3}}
			},
		},
		{
			name: "capture beyond per-job limit",
			mutate: func(state *generated.PersistedJobState) {
				state.Job.OutputLimitBytes = 1
			},
		},
		{
			name: "workspace root is slash",
			mutate: func(state *generated.PersistedJobState) {
				state.Session.WorkspacePath = "/"
				state.Job.WorkspacePath = "/"
				state.Job.Cwd = "/"
			},
		},
		{
			name: "cwd outside workspace",
			mutate: func(state *generated.PersistedJobState) {
				state.Job.Cwd = "/outside"
			},
		},
		{
			name: "noncanonical cwd",
			mutate: func(state *generated.PersistedJobState) {
				state.Job.Cwd = "/workspace/../workspace"
			},
		},
		{
			name: "cancellation requester is not owner",
			mutate: func(state *generated.PersistedJobState) {
				state.Cancellation = &generated.CancellationMetadata{Requested: true, RequestedBySessionID: "session-2"}
			},
		},
		{
			name: "cancellation requested flag is false",
			mutate: func(state *generated.PersistedJobState) {
				state.Cancellation = &generated.CancellationMetadata{Requested: false, RequestedBySessionID: "session-1"}
			},
		},
		{
			name: "observer updated after terminal finish",
			mutate: func(state *generated.PersistedJobState) {
				state.Observers = []generated.ObserverCursor{{SessionID: "observer", CursorBytes: 2, UpdatedAtUnixMs: 11}}
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			state := validSucceededState()
			if testCase.name == "running state with result" {
				state = validRunningState()
			}
			testCase.mutate(&state)

			decision := policy.ValidatePersistedState(state)

			require.Equal(t, Decision{Allowed: false, Code: CodeCorruptState}, decision)
		})
	}
}

func validRunningState() generated.PersistedJobState {
	return generated.PersistedJobState{
		SchemaVersion: 1,
		Session: generated.SessionMetadata{
			SchemaVersion: 1,
			SessionID:     "session-1",
			WorkspacePath: "/workspace",
		},
		Job: generated.JobMetadata{
			JobID:            "job-1",
			Status:           generated.JobStatusRunning,
			OwnerSessionID:   "session-1",
			WorkspacePath:    "/workspace",
			Cwd:              "/workspace",
			Command:          "printf ok",
			CapturedBytes:    2,
			HardTimeoutMs:    1000,
			OutputLimitBytes: 10,
		},
		Observers: []generated.ObserverCursor{},
	}
}

func validSucceededState() generated.PersistedJobState {
	state := validRunningState()
	finished := generated.TimestampUnixMs(10)
	exitCode := 0
	state.Job.Status = generated.JobStatusSucceeded
	state.Job.FinishedAtUnixMs = &finished
	state.Result = &generated.ProcessResult{
		Status:           generated.TerminalStatusSucceeded,
		FinishedAtUnixMs: finished,
		CapturedBytes:    2,
		ExitCode:         &exitCode,
	}
	return state
}

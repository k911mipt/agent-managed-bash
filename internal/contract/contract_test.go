package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
)

func Test_Load_returns_checked_in_policy_and_validator(t *testing.T) {
	// Given
	workspace := filepath.Join(t.TempDir(), "workspace")
	cwd := filepath.Join(workspace, "cwd")
	require.NoError(t, os.MkdirAll(cwd, 0o700))
	contracts, err := Load()
	require.NoError(t, err)
	persisted := validStoredState(workspace, cwd)
	raw, err := json.Marshal(persisted)
	require.NoError(t, err)

	// When
	validated, decision := contracts.StateValidator().ValidateStored(raw, workspace)

	// Then
	require.Equal(t, state.Decision{Allowed: true, Code: state.CodeAllow}, decision)
	require.Equal(t, persisted, validated)
	require.Equal(t, state.ByteCount(104857600), contracts.Policy().CaptureLimit())
}

func Test_StateValidator_accepts_historical_cwd_after_job_removes_it(t *testing.T) {
	// Given
	workspace := filepath.Join(t.TempDir(), "workspace")
	cwd := filepath.Join(workspace, "cwd")
	require.NoError(t, os.MkdirAll(cwd, 0o700))
	contracts, err := Load()
	require.NoError(t, err)
	raw, err := json.Marshal(validStoredState(workspace, cwd))
	require.NoError(t, err)
	require.NoError(t, os.Remove(cwd))

	// When
	_, decision := contracts.StateValidator().ValidateStored(raw, workspace)

	// Then
	require.Equal(t, state.Decision{Allowed: true, Code: state.CodeAllow}, decision)
}

func validStoredState(workspace string, cwd string) generated.PersistedJobState {
	return generated.PersistedJobState{
		SchemaVersion: 1,
		Session: generated.SessionMetadata{
			SchemaVersion: 1, SessionID: "session-1", WorkspacePath: workspace, CreatedAtUnixMs: 100,
		},
		Job: generated.JobMetadata{
			JobID: "job-1", Status: generated.JobStatusRunning, OwnerSessionID: "session-1",
			WorkspacePath: workspace, Cwd: cwd, Command: "printf ok", CreatedAtUnixMs: 101,
			StartedAtUnixMs: 102, HardTimeoutMs: 1000, OutputLimitBytes: 10,
		},
		Observers: []generated.ObserverCursor{},
	}
}

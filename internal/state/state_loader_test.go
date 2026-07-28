package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_PersistedStateValidator_rejects_malformed_structural_and_semantic_state(t *testing.T) {
	validator := newTestPersistedStateValidator(t)
	workspace := filepath.Join(testWorkspaceRoot(t, os.TempDir()), "workspace")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "malformed", raw: []byte(`{"schema_version":`)},
		{name: "trailing", raw: append(readStateFixture(t, "valid/state-running.json"), []byte(` {}`)...)},
		{name: "unknown field", raw: readStateFixture(t, "invalid/state-corrupt-environment.json")},
		{name: "invalid id", raw: readStateFixture(t, "invalid/state-invalid-job-id.json")},
		{name: "semantic cwd escape", raw: readPolicyStateFixture(t, "invalid-cwd-outside.json")},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			raw := bytes.ReplaceAll(testCase.raw, []byte("/workspace"), []byte(workspace))
			_, decision := validator.Validate(raw, workspace)
			require.Equal(t, Decision{Allowed: false, Code: CodeCorruptState}, decision)
		})
	}
}

func Test_PersistedStateValidator_accepts_valid_raw_state(t *testing.T) {
	validator := newTestPersistedStateValidator(t)
	workspace := filepath.Join(testWorkspaceRoot(t, os.TempDir()), "workspace")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	raw := bytes.ReplaceAll(readStateFixture(t, "valid/state-running.json"), []byte("/workspace"), []byte(workspace))

	state, decision := validator.Validate(raw, workspace)

	require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
	require.Equal(t, "job-1", string(state.Job.JobID))
}

func Test_PersistedStateValidator_binds_stored_paths_to_host_workspace(t *testing.T) {
	// Given
	validator := newTestPersistedStateValidator(t)
	base := testWorkspaceRoot(t, os.TempDir())
	workspace := filepath.Join(base, "workspace")
	otherWorkspace := filepath.Join(base, "other-workspace")
	cwd := filepath.Join(workspace, "cwd")
	require.NoError(t, os.MkdirAll(cwd, 0o700))
	require.NoError(t, os.Mkdir(otherWorkspace, 0o700))
	state := validRunningState()
	state.Session.WorkspacePath = workspace
	state.Job.WorkspacePath = workspace
	state.Job.Cwd = cwd
	raw, err := json.Marshal(state)
	require.NoError(t, err)

	tests := []struct {
		name          string
		hostWorkspace string
		expected      Decision
	}{
		{name: "matching host workspace", hostWorkspace: workspace, expected: Decision{Allowed: true, Code: CodeAllow}},
		{
			name: "different host workspace", hostWorkspace: otherWorkspace,
			expected: Decision{Allowed: false, Code: CodeCorruptState},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// When
			_, decision := validator.Validate(raw, testCase.hostWorkspace)

			// Then
			require.Equal(t, testCase.expected, decision)
		})
	}
}

func Test_PersistedStateValidator_rejects_symlinked_stored_cwd(t *testing.T) {
	// Given
	validator := newTestPersistedStateValidator(t)
	workspace := filepath.Join(testWorkspaceRoot(t, os.TempDir()), "workspace")
	realCwd := filepath.Join(workspace, "real-cwd")
	linkedCwd := filepath.Join(workspace, "linked-cwd")
	require.NoError(t, os.MkdirAll(realCwd, 0o700))
	require.NoError(t, os.Symlink(realCwd, linkedCwd))
	state := validRunningState()
	state.Session.WorkspacePath = workspace
	state.Job.WorkspacePath = workspace
	state.Job.Cwd = linkedCwd
	raw, err := json.Marshal(state)
	require.NoError(t, err)

	// When
	_, decision := validator.Validate(raw, workspace)

	// Then
	require.Equal(t, Decision{Allowed: false, Code: CodeCorruptState}, decision)
}

func Test_PersistedStateValidator_rejects_symlinked_stored_workspace(t *testing.T) {
	// Given
	validator := newTestPersistedStateValidator(t)
	base := testWorkspaceRoot(t, os.TempDir())
	realWorkspace := filepath.Join(base, "real-workspace")
	linkedWorkspace := filepath.Join(base, "linked-workspace")
	require.NoError(t, os.Mkdir(realWorkspace, 0o700))
	require.NoError(t, os.Symlink(realWorkspace, linkedWorkspace))
	state := validRunningState()
	state.Session.WorkspacePath = linkedWorkspace
	state.Job.WorkspacePath = linkedWorkspace
	state.Job.Cwd = linkedWorkspace
	raw, err := json.Marshal(state)
	require.NoError(t, err)

	// When
	_, decision := validator.Validate(raw, linkedWorkspace)

	// Then
	require.Equal(t, Decision{Allowed: false, Code: CodeCorruptState}, decision)
}

func newTestPersistedStateValidator(t *testing.T) PersistedStateValidator {
	t.Helper()
	root := filepath.Join("..", "..", "schemas", "v1")
	models, err := os.ReadFile(filepath.Join(root, "models.schema.json"))
	require.NoError(t, err)
	stateSchema, err := os.ReadFile(filepath.Join(root, "state.schema.json"))
	require.NoError(t, err)
	validator, err := NewPersistedStateValidator(models, stateSchema, loadTestPolicy(t))
	require.NoError(t, err)
	return validator
}

func readStateFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "v1", "schema", path))
	require.NoError(t, err)
	return raw
}

func readPolicyStateFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "v1", "policy", "state", path))
	require.NoError(t, err)
	return raw
}

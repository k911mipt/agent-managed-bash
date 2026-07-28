package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func loadTestPolicy(t *testing.T) Policy {
	t.Helper()

	schema, document := readTestPolicyDocuments(t)
	policy, err := LoadPolicy(schema, document)
	require.NoError(t, err)
	return policy
}

func readTestPolicyDocuments(t *testing.T) ([]byte, []byte) {
	t.Helper()

	repositoryRoot := filepath.Join("..", "..")
	schema, err := os.ReadFile(filepath.Join(repositoryRoot, "schemas", "v1", "policy.schema.json"))
	require.NoError(t, err)
	document, err := os.ReadFile(filepath.Join(repositoryRoot, "schemas", "v1", "policy.json"))
	require.NoError(t, err)
	return schema, document
}

func testWorkspaceRoot(t *testing.T, temporaryRoot string) string {
	t.Helper()
	physicalTemporaryRoot, err := filepath.EvalSymlinks(temporaryRoot)
	require.NoError(t, err)
	workspace, err := os.MkdirTemp(physicalTemporaryRoot, "state-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(workspace)) })
	return workspace
}

func Test_testWorkspaceRoot_returns_physical_path_when_temporary_root_is_symlinked(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)
	testRoot := t.TempDir()
	physicalTemporaryRoot := filepath.Join(testRoot, "physical")
	linkedTemporaryRoot := filepath.Join(testRoot, "linked")
	require.NoError(t, os.Mkdir(physicalTemporaryRoot, 0o700))
	require.NoError(t, os.Symlink(physicalTemporaryRoot, linkedTemporaryRoot))
	workspace := testWorkspaceRoot(t, linkedTemporaryRoot)
	candidate := filepath.Join(workspace, "file")
	require.NoError(t, os.WriteFile(candidate, []byte("ok"), 0o600))

	// When
	file, decision := policy.OpenWorkspacePath(workspace, candidate)
	if file != nil {
		t.Cleanup(func() { require.NoError(t, file.Close()) })
	}

	// Then
	require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
}

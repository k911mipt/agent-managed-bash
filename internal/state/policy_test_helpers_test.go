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

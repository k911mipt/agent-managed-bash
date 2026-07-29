//go:build darwin

package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_renameNoReplace_publishes_writable_directory(t *testing.T) {
	root := physicalTempDir(t)
	staged := filepath.Join(root, "staged")
	published := filepath.Join(root, "published")
	require.NoError(t, os.Mkdir(staged, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(staged, "artifact"), []byte("artifact"), 0o444))

	err := renameNoReplace(staged, published)

	require.NoError(t, err)
	require.NoDirExists(t, staged)
	require.FileExists(t, filepath.Join(published, "artifact"))
}

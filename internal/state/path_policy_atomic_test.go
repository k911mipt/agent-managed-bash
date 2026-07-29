//go:build linux || darwin

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_OpenWorkspacePath_rejects_workspace_replaced_by_symlink_before_open(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)
	base := testWorkspaceRoot(t, os.TempDir())
	workspace := filepath.Join(base, "workspace")
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	require.NoError(t, os.Mkdir(outside, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "secret"), []byte("inside"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0o600))
	replaced := false
	opener := func(directoryFD int, component string, directory bool) (int, error) {
		if component == "workspace" && !replaced {
			replaced = true
			require.NoError(t, os.Rename(workspace, workspace+"-original"))
			require.NoError(t, os.Symlink(outside, workspace))
		}
		return openPathComponent(directoryFD, component, directory)
	}

	// When
	file, decision := policy.openWorkspacePathWith(
		pathOpenRequest{
			workspacePath: workspace, candidatePath: filepath.Join(workspace, "secret"), expected: finalRegularFile,
		},
		opener,
	)

	// Then
	require.Nil(t, file)
	require.Equal(t, Decision{Allowed: false, Code: CodePathSymlink}, decision)
}

func Test_OpenWorkspacePath_rejects_component_replaced_by_same_inode_symlink_before_open(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)
	workspace := filepath.Join(testWorkspaceRoot(t, os.TempDir()), "workspace")
	subdirectory := filepath.Join(workspace, "sub")
	require.NoError(t, os.MkdirAll(subdirectory, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(subdirectory, "file"), []byte("inside"), 0o600))
	replaced := false
	opener := func(directoryFD int, component string, directory bool) (int, error) {
		if component == "sub" && !replaced {
			replaced = true
			require.NoError(t, os.Rename(subdirectory, subdirectory+"-original"))
			require.NoError(t, os.Symlink("sub-original", subdirectory))
		}
		return openPathComponent(directoryFD, component, directory)
	}

	// When
	file, decision := policy.openWorkspacePathWith(
		pathOpenRequest{
			workspacePath: workspace,
			candidatePath: filepath.Join(workspace, "sub", "file"),
			expected:      finalRegularFile,
		},
		opener,
	)

	// Then
	require.Nil(t, file)
	require.Equal(t, Decision{Allowed: false, Code: CodePathSymlink}, decision)
}

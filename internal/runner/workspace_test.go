//go:build linux || darwin

package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const maxTestWorkspacePathLength = 64

func testWorkspace(t *testing.T) string {
	t.Helper()

	physicalTemporaryRoot, err := filepath.EvalSymlinks("/tmp")
	require.NoError(t, err)
	workspace, err := os.MkdirTemp(physicalTemporaryRoot, "amb-runner-")
	require.NoError(t, err)
	physicalWorkspace, err := filepath.EvalSymlinks(workspace)
	require.NoError(t, err)
	require.LessOrEqual(t, len(physicalWorkspace), maxTestWorkspacePathLength)
	require.NoError(t, os.Chmod(physicalWorkspace, 0o700))
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(physicalWorkspace)) })
	return physicalWorkspace
}

func NewTestWorkspace(t *testing.T) string {
	t.Helper()
	return testWorkspace(t)
}

func Test_testWorkspace_returns_physical_path_when_TMPDIR_is_symlinked(t *testing.T) {
	// Given
	testRoot := testWorkspace(t)
	physicalTMPDIR := filepath.Join(testRoot, "physical-tmp")
	linkedTMPDIR := filepath.Join(testRoot, "linked-tmp")
	require.NoError(t, os.Mkdir(physicalTMPDIR, 0o700))
	require.NoError(t, os.Symlink(physicalTMPDIR, linkedTMPDIR))
	t.Setenv("TMPDIR", linkedTMPDIR)

	t.Run("raw TempDir remains rejected", func(t *testing.T) {
		// Given
		workspace := t.TempDir()
		require.True(t, strings.HasPrefix(workspace, linkedTMPDIR+string(filepath.Separator)))

		// When
		directory, err := openWorkspaceDirectory(workspace)
		if directory != nil {
			t.Cleanup(func() { require.NoError(t, directory.Close()) })
		}

		// Then
		require.ErrorIs(t, err, ErrUnsafeFilesystem)
	})

	t.Run("test workspace is accepted", func(t *testing.T) {
		// Given
		workspace := testWorkspace(t)

		// When
		directory, err := openWorkspaceDirectory(workspace)
		if directory != nil {
			t.Cleanup(func() { require.NoError(t, directory.Close()) })
		}

		// Then
		require.NoError(t, err)
	})
}

func Test_openWorkspaceDirectory_rejects_explicit_workspace_symlink(t *testing.T) {
	// Given
	workspace := testWorkspace(t)
	linkedWorkspace := filepath.Join(testWorkspace(t), "workspace-link")
	require.NoError(t, os.Symlink(workspace, linkedWorkspace))

	// When
	directory, err := openWorkspaceDirectory(linkedWorkspace)
	if directory != nil {
		t.Cleanup(func() { require.NoError(t, directory.Close()) })
	}

	// Then
	require.ErrorIs(t, err, ErrUnsafeFilesystem)
}

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_OpenWorkspacePath_matches_real_filesystem_cases(t *testing.T) {
	policy := loadTestPolicy(t)
	cases := loadPolicyCases(t).Paths
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	workspace := filepath.Join(base, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "sub"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "sub", "file"), []byte("ok"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "outside"), []byte("no"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(workspace, "sub", "file"), filepath.Join(workspace, "file-link")))
	require.NoError(t, os.Symlink(filepath.Join(workspace, "sub"), filepath.Join(workspace, "dir-link")))
	workspaceLink := filepath.Join(base, "workspace-link")
	require.NoError(t, os.Symlink(workspace, workspaceLink))

	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			// Given
			workspacePath := workspace
			candidate := pathCandidate(testCase.Kind, base, workspace)
			if testCase.Kind == "symlink_workspace" {
				workspacePath = workspaceLink
				candidate = filepath.Join(workspaceLink, "sub", "file")
			}

			// When
			file, decision := policy.OpenWorkspacePath(workspacePath, candidate)
			if file != nil {
				t.Cleanup(func() { require.NoError(t, file.Close()) })
			}

			// Then
			require.Equal(t, Decision{Allowed: testCase.Allowed, Code: testCase.Code}, decision)
		})
	}
}

func Test_OpenWorkspacePath_rejects_untrusted_root_workspace(t *testing.T) {
	policy := loadTestPolicy(t)

	file, decision := policy.OpenWorkspacePath("/", "/")

	require.Nil(t, file)
	require.Equal(t, Decision{Allowed: false, Code: CodePathInvalid}, decision)
}

func pathCandidate(kind string, base string, workspace string) string {
	switch kind {
	case "regular":
		return filepath.Join(workspace, "sub", "file")
	case "workspace_root":
		return workspace
	case "outside":
		return filepath.Join(base, "outside")
	case "traversal":
		return workspace + string(filepath.Separator) + "sub" + string(filepath.Separator) + ".." + string(filepath.Separator) + "sub" + string(filepath.Separator) + "file"
	case "relative":
		return filepath.Join("sub", "file")
	case "symlink_file":
		return filepath.Join(workspace, "file-link")
	case "symlink_directory":
		return filepath.Join(workspace, "dir-link", "file")
	case "symlink_workspace":
		return filepath.Join(workspace, "sub", "file")
	case "missing":
		return filepath.Join(workspace, "missing")
	default:
		return ""
	}
}

//go:build linux || darwin

package state

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_OpenWorkspacePath_rejects_FIFO_without_blocking(t *testing.T) {
	policy := loadTestPolicy(t)
	workspace := filepath.Join(testWorkspaceRoot(t, os.TempDir()), "workspace")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	fifo := filepath.Join(workspace, "fifo")
	require.NoError(t, unix.Mkfifo(fifo, 0o600))

	file, decision := policy.OpenWorkspacePath(workspace, fifo)

	require.Nil(t, file)
	require.Equal(t, Decision{Allowed: false, Code: CodePathUnavailable}, decision)
}

func Test_OpenWorkspacePath_rejects_unix_socket(t *testing.T) {
	policy := loadTestPolicy(t)
	workspace := filepath.Join(testWorkspaceRoot(t, os.TempDir()), "workspace")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	socketPath := filepath.Join(workspace, "socket")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })

	file, decision := policy.OpenWorkspacePath(workspace, socketPath)

	require.Nil(t, file)
	require.Equal(t, Decision{Allowed: false, Code: CodePathUnavailable}, decision)
}

func Test_OpenWorkspacePath_rejects_all_directories(t *testing.T) {
	policy := loadTestPolicy(t)
	workspace := filepath.Join(testWorkspaceRoot(t, os.TempDir()), "workspace")
	nested := filepath.Join(workspace, "nested")
	require.NoError(t, os.MkdirAll(nested, 0o700))

	nestedFile, nestedDecision := policy.OpenWorkspacePath(workspace, nested)
	rootFile, rootDecision := policy.OpenWorkspacePath(workspace, workspace)
	require.Nil(t, nestedFile)
	require.Equal(t, Decision{Allowed: false, Code: CodePathUnavailable}, nestedDecision)
	require.Nil(t, rootFile)
	require.Equal(t, Decision{Allowed: false, Code: CodePathUnavailable}, rootDecision)
}

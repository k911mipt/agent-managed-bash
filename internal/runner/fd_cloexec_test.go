//go:build linux || darwin

package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_duplicateDirectory_sets_close_on_exec_atomically(t *testing.T) {
	directory, err := openWorkspaceDirectory(testWorkspace(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, directory.Close()) })

	duplicated, err := duplicateDirectory(directory, "duplicate")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, duplicated.Close()) })
	flags, err := unix.FcntlInt(duplicated.Fd(), unix.F_GETFD, 0)
	require.NoError(t, err)
	require.NotZero(t, flags&unix.FD_CLOEXEC)
}

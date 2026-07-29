//go:build darwin

package runner

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_preparePipeReader_clears_nonblocking_flag(t *testing.T) {
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(reader.Close(), writer.Close())) })
	require.True(t, pipeReaderIsNonblocking(t, reader))

	err = preparePipeReader(reader)

	require.NoError(t, err)
	require.False(t, pipeReaderIsNonblocking(t, reader))
}

func pipeReaderIsNonblocking(t *testing.T, reader *os.File) bool {
	t.Helper()
	raw, err := reader.SyscallConn()
	require.NoError(t, err)
	flags := 0
	var controlErr error
	require.NoError(t, raw.Control(func(fd uintptr) {
		flags, controlErr = unix.FcntlInt(fd, unix.F_GETFL, 0)
	}))
	require.NoError(t, controlErr)
	return flags&unix.O_NONBLOCK != 0
}

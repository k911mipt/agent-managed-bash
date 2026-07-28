//go:build darwin

package runner

import (
	"os"

	"golang.org/x/sys/unix"
)

func preparePipeReader(reader *os.File) error {
	fd := reader.Fd()
	return unix.SetNonblock(int(fd), false)
}

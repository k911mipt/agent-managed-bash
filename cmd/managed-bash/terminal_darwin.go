//go:build darwin

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func stdinIsTerminal(stdin *os.File) bool {
	_, err := unix.IoctlGetTermios(int(stdin.Fd()), unix.TIOCGETA)
	return err == nil
}

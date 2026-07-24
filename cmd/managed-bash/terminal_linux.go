//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func stdinIsTerminal(stdin *os.File) bool {
	_, err := unix.IoctlGetTermios(int(stdin.Fd()), unix.TCGETS)
	return err == nil
}

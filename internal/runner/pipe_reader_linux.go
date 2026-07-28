//go:build linux

package runner

import "os"

func preparePipeReader(*os.File) error {
	return nil
}

//go:build linux

package runner

func benignProcessGroupSignalError(int, error) bool {
	return false
}

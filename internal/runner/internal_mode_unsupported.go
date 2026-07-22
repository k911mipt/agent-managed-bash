//go:build !linux && !darwin

package runner

import "context"

func DispatchInternal(_ context.Context, args []string) (bool, error) {
	if len(args) == 1 && (args[0] == "--managed-bash-internal=bootstrap" || args[0] == "--managed-bash-internal=runner" ||
		args[0] == "--managed-bash-internal=guardian") {
		return true, ErrUnsupported
	}
	return false, nil
}

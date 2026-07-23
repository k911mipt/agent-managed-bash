//go:build linux || darwin

package installer

import "context"

const (
	internalInstallArgument   = "--managed-bash-internal=install"
	internalUninstallArgument = "--managed-bash-internal=uninstall"
)

func Dispatch(ctx context.Context, args []string, binaryVersion string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case internalInstallArgument:
		if len(args) != 3 || args[1] != "--bundle-root" || args[2] == "" {
			return true, ErrInvalidArguments
		}
		return true, Install(ctx, Config{BundleRoot: args[2], BinaryVersion: binaryVersion})
	case internalUninstallArgument:
		if len(args) != 1 {
			return true, ErrInvalidArguments
		}
		return true, Uninstall(ctx, Config{BinaryVersion: binaryVersion})
	default:
		return false, nil
	}
}

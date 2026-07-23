//go:build linux || darwin

package installer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/k911mipt/agent-managed-bash/internal/release"
)

var installedVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func Install(ctx context.Context, config Config) error {
	return installWithHooks(ctx, config, hooks{})
}

func installWithHooks(ctx context.Context, config Config, callbacks hooks) (err error) {
	paths, err := pathsFromConfig(config)
	if err != nil {
		return err
	}
	lock, err := acquireInstallationLock(ctx, paths, callbacks)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.close()) }()
	if err := ensureDirectory(paths.dataRoot, 0o700); err != nil {
		return err
	}
	if err := validateOwnedDirectory(paths.dataRoot, 0o700); err != nil {
		return err
	}
	bundle, err := release.VerifyBundle(release.BundleConfig{
		Root: config.BundleRoot, Version: config.BinaryVersion, OS: runtime.GOOS, Architecture: runtime.GOARCH,
	})
	if err != nil {
		return err
	}
	installedIdentity := identityFromBundle(bundle)
	binTarget := filepath.Join(paths.dataRoot, "current", "bin", "managed-bash")
	pluginTarget := filepath.Join(paths.dataRoot, "current", "lib", "opencode", "managed-bash.js")
	binExists, err := preflightLink(paths.binLink, binTarget)
	if err != nil {
		return err
	}
	pluginExists, err := preflightLink(paths.pluginLink, pluginTarget)
	if err != nil {
		return err
	}
	oldPointer, err := readCurrent(paths)
	if err != nil {
		return err
	}
	final, releaseCreated, err := prepareRelease(paths, bundle, callbacks.beforeReleaseRename)
	if err != nil {
		return err
	}
	if err := verifyBinary(ctx, filepath.Join(final, "bin", "managed-bash"), installedIdentity); err != nil {
		return errors.Join(err, removeCreatedRelease(final, releaseCreated))
	}
	newTarget := currentReleaseTarget(installedIdentity)
	if oldPointer.exists && oldPointer.target == newTarget && binExists && pluginExists {
		return verifyInstalled(ctx, callbacks, filepath.Join(paths.dataRoot, "current", "bin", "managed-bash"), installedIdentity)
	}
	if callbacks.beforeCommit != nil {
		if err := callbacks.beforeCommit(); err != nil {
			return errors.Join(err, removeCreatedRelease(final, releaseCreated))
		}
	}
	createdBin := false
	createdPlugin := false
	rollbackInitial := func(cause error) error {
		removeLink := func(path string, target string, created bool) error {
			if created && callbacks.beforeLinkCleanup != nil {
				if err := callbacks.beforeLinkCleanup(path); err != nil {
					return err
				}
			}
			return removeCreatedLink(path, target, created)
		}
		pluginErr := removeLink(paths.pluginLink, pluginTarget, createdPlugin)
		binErr := removeLink(paths.binLink, binTarget, createdBin)
		if pluginErr != nil || binErr != nil {
			return errors.Join(cause, pluginErr, binErr)
		}
		return errors.Join(cause, removeCreatedRelease(final, releaseCreated))
	}
	if !binExists {
		published, err := publishLink(paths.binLink, binTarget, callbacks.beforeLinkRename, callbacks.afterLinkRename)
		createdBin = published
		if err != nil {
			return rollbackInitial(err)
		}
	}
	if !pluginExists {
		if callbacks.beforePluginLink != nil {
			if err := callbacks.beforePluginLink(); err != nil {
				return rollbackInitial(err)
			}
		}
		published, err := publishLink(paths.pluginLink, pluginTarget, callbacks.beforeLinkRename, callbacks.afterLinkRename)
		createdPlugin = published
		if err != nil {
			return rollbackInitial(err)
		}
	}
	if oldPointer.target != newTarget {
		switched, err := switchCurrent(paths, oldPointer, newTarget, callbacks.afterCurrentRename)
		if err != nil {
			if switched {
				if rollbackErr := restoreCurrent(paths, newTarget, oldPointer); rollbackErr != nil {
					return errors.Join(err, rollbackErr)
				}
			}
			return rollbackInitial(err)
		}
	}
	if err := verifyInstalled(ctx, callbacks, filepath.Join(paths.dataRoot, "current", "bin", "managed-bash"), installedIdentity); err != nil {
		rollbackErr := restoreCurrent(paths, newTarget, oldPointer)
		if rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		cleanupErr := rollbackInitial(nil)
		return errors.Join(err, cleanupErr)
	}
	return nil
}

func verifyInstalled(ctx context.Context, callbacks hooks, path string, expected identity) error {
	if callbacks.verifyInstalled != nil {
		return callbacks.verifyInstalled(ctx, path, expected)
	}
	return verifyBinary(ctx, path, expected)
}

func readCurrent(paths installPaths) (currentPointer, error) {
	info, err := os.Lstat(paths.current)
	if errors.Is(err, os.ErrNotExist) {
		return currentPointer{}, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return currentPointer{}, fmt.Errorf("inspect current pointer: %w", errors.Join(err, ErrForeignPath))
	}
	target, err := os.Readlink(paths.current)
	if err != nil || !ownedCurrentTarget(target) {
		return currentPointer{}, fmt.Errorf("read current pointer: %w", errors.Join(err, ErrForeignPath))
	}
	root := filepath.Join(paths.dataRoot, filepath.FromSlash(target))
	if err := validateInstalledTree(root, nil); err != nil {
		return currentPointer{}, err
	}
	return currentPointer{target: target, exists: true}, nil
}

func ownedCurrentTarget(target string) bool {
	prefix := "releases/"
	if !strings.HasPrefix(target, prefix) || strings.Contains(strings.TrimPrefix(target, prefix), "/") {
		return false
	}
	suffix := "-" + hostTarget()
	name := strings.TrimPrefix(target, prefix)
	if !strings.HasSuffix(name, suffix) {
		return false
	}
	return installedVersionPattern.MatchString(strings.TrimSuffix(name, suffix))
}

func switchCurrent(paths installPaths, expected currentPointer, newTarget string, afterRename func() error) (bool, error) {
	actual, err := readCurrent(paths)
	if err != nil {
		return false, err
	}
	if actual != expected {
		return false, fmt.Errorf("current pointer changed during install: %w", ErrForeignPath)
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return false, fmt.Errorf("generate current pointer name: %w", err)
	}
	temporary := filepath.Join(paths.dataRoot, ".current-"+hex.EncodeToString(suffix))
	if err := os.Symlink(newTarget, temporary); err != nil {
		return false, fmt.Errorf("prepare current pointer: %w", err)
	}
	defer os.Remove(temporary)
	if err := os.Rename(temporary, paths.current); err != nil {
		return false, fmt.Errorf("switch current pointer: %w", err)
	}
	if afterRename != nil {
		if err := afterRename(); err != nil {
			return true, err
		}
	}
	return true, syncDirectory(paths.dataRoot)
}

func restoreCurrent(paths installPaths, installedTarget string, old currentPointer) error {
	actual, err := readCurrent(paths)
	if err != nil {
		return err
	}
	if !actual.exists || actual.target != installedTarget {
		return fmt.Errorf("cannot restore changed current pointer: %w", ErrForeignPath)
	}
	if old.exists {
		_, err := switchCurrent(paths, actual, old.target, nil)
		return err
	}
	if err := os.Remove(paths.current); err != nil {
		return fmt.Errorf("remove failed initial current pointer: %w", err)
	}
	return syncDirectory(paths.dataRoot)
}

func removeCreatedLink(path string, target string, created bool) error {
	if !created {
		return nil
	}
	owned, err := preflightLink(path, target)
	if err != nil || !owned {
		return errors.Join(err, ErrForeignPath)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove registration %q: %w", path, err)
	}
	return syncDirectory(filepath.Dir(path))
}

func removeCreatedRelease(path string, created bool) error {
	if !created {
		return nil
	}
	return removeRelease(path)
}

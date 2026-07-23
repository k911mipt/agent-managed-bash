//go:build linux || darwin

package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func Uninstall(ctx context.Context, config Config) error {
	return uninstallWithHooks(ctx, config, hooks{})
}

func uninstallWithHooks(ctx context.Context, config Config, callbacks hooks) error {
	paths, err := pathsFromConfig(config)
	if err != nil {
		return err
	}
	lock, err := acquireInstallationLock(ctx, paths, callbacks)
	if err != nil {
		return err
	}
	binTarget := filepath.Join(paths.dataRoot, "current", "bin", "managed-bash")
	pluginTarget := filepath.Join(paths.dataRoot, "current", "lib", "opencode", "managed-bash.js")
	operationErr := uninstallLocked(paths, binTarget, pluginTarget, callbacks)
	if operationErr != nil {
		return errors.Join(operationErr, lock.close())
	}
	if callbacks.beforeLockUnlink != nil {
		callbacks.beforeLockUnlink()
	}
	unlinkErr := lock.unlinkNamed()
	return errors.Join(unlinkErr, lock.close())
}

func uninstallLocked(paths installPaths, binTarget string, pluginTarget string, callbacks hooks) error {
	binExists, err := preflightLink(paths.binLink, binTarget)
	if err != nil {
		return err
	}
	legacyPluginExists, err := preflightLink(paths.legacyPluginLink, pluginTarget)
	if err != nil {
		return err
	}
	dataExists, err := pathExists(paths.dataRoot)
	if err != nil {
		return err
	}
	current := currentPointer{}
	var releases []string
	if dataExists {
		current, err = readCurrent(paths)
		if err != nil {
			return err
		}
		releases, err = validateOwnedInstallation(paths)
		if err != nil {
			return err
		}
	}
	if callbacks.beforeUninstallCommit != nil {
		callbacks.beforeUninstallCommit()
	}
	if err := validateLinkState(paths.legacyPluginLink, pluginTarget, legacyPluginExists); err != nil {
		return err
	}
	if err := validateLinkState(paths.binLink, binTarget, binExists); err != nil {
		return err
	}
	if legacyPluginExists {
		if _, err := removeExpectedLink(paths.legacyPluginLink, pluginTarget, true, callbacks); err != nil {
			return err
		}
	}
	if binExists {
		if _, err := removeExpectedLink(paths.binLink, binTarget, true, callbacks); err != nil {
			return err
		}
	}
	if current.exists {
		if err := os.Remove(paths.current); err != nil {
			return fmt.Errorf("remove current pointer: %w", err)
		}
	}
	for _, releaseRoot := range releases {
		if err := removeRelease(releaseRoot); err != nil {
			return err
		}
	}
	if dataExists {
		if err := os.Remove(paths.releases); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove releases directory: %w", err)
		}
		if err := os.Remove(paths.dataRoot); err != nil {
			return fmt.Errorf("remove installation data root: %w", err)
		}
		return syncDirectory(paths.dataHome)
	}
	return nil
}

func validateOwnedInstallation(paths installPaths) ([]string, error) {
	entries, err := os.ReadDir(paths.dataRoot)
	if err != nil {
		return nil, fmt.Errorf("read installation data root: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() != "current" && entry.Name() != "releases" {
			return nil, fmt.Errorf("unknown installation entry %q: %w", entry.Name(), ErrForeignPath)
		}
	}
	if _, err := os.Lstat(paths.releases); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect releases directory: %w", err)
	}
	if err := validateOwnedDirectory(paths.releases, 0o700); err != nil {
		return nil, err
	}
	releaseEntries, err := os.ReadDir(paths.releases)
	if err != nil {
		return nil, fmt.Errorf("read releases directory: %w", err)
	}
	releases := make([]string, 0, len(releaseEntries))
	for _, entry := range releaseEntries {
		if !ownedCurrentTarget("releases/"+entry.Name()) || !entry.IsDir() {
			return nil, fmt.Errorf("unknown release %q: %w", entry.Name(), ErrForeignPath)
		}
		root := filepath.Join(paths.releases, entry.Name())
		if err := validateInstalledTree(root, nil); err != nil {
			return nil, err
		}
		releases = append(releases, root)
	}
	return releases, nil
}

func pathExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect path %q: %w", path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("path %q: %w", path, ErrUnsafePath)
	}
	return true, nil
}

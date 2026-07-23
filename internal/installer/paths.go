//go:build linux || darwin

package installer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func preflightLink(path string, ownedTarget string) (bool, error) {
	if err := validatePathComponents(filepath.Dir(path)); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect registration %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, fmt.Errorf("registration %q: %w", path, ErrForeignPath)
	}
	if err := validateRegistrationOwner(info); err != nil {
		return false, fmt.Errorf("registration %q ownership: %w", path, err)
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false, fmt.Errorf("read registration %q: %w", path, err)
	}
	if target != ownedTarget {
		return false, fmt.Errorf("registration %q targets %q: %w", path, target, ErrForeignPath)
	}
	return true, nil
}

func validateRegistrationOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return ErrForeignPath
	}
	return nil
}

func validateLinkState(path string, target string, expected bool) error {
	exists, err := preflightLink(path, target)
	if err != nil {
		return err
	}
	if exists != expected {
		return fmt.Errorf("registration %q changed during operation: %w", path, ErrForeignPath)
	}
	return nil
}

func removeExpectedLink(path string, target string, expected bool, beforeRemove func(string) error) (bool, error) {
	if err := validateLinkState(path, target, expected); err != nil || !expected {
		return false, err
	}
	if beforeRemove != nil {
		if err := beforeRemove(path); err != nil {
			return false, err
		}
	}
	if err := validateLinkState(path, target, true); err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("remove registration %q: %w", path, err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return true, err
	}
	return true, nil
}

func publishLink(path string, target string, beforeRename func(string), afterRename func(string) error) (bool, error) {
	if err := ensureDirectory(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return false, fmt.Errorf("generate link name: %w", err)
	}
	temporary := filepath.Join(filepath.Dir(path), ".managed-bash-link-"+hex.EncodeToString(suffix))
	if err := os.Symlink(target, temporary); err != nil {
		return false, fmt.Errorf("prepare registration %q: %w", path, err)
	}
	defer os.Remove(temporary)
	if beforeRename != nil {
		beforeRename(path)
	}
	if err := renameNoReplace(temporary, path); errors.Is(err, unix.EEXIST) {
		return false, fmt.Errorf("registration %q appeared during publication: %w", path, ErrForeignPath)
	} else if err != nil {
		return false, fmt.Errorf("publish registration %q: %w", path, err)
	}
	if afterRename != nil {
		if err := afterRename(path); err != nil {
			return true, err
		}
	}
	return true, syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %q for sync: %w", path, err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory %q: %w", path, err)
	}
	return nil
}

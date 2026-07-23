//go:build linux || darwin

package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func validatePathComponents(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return ErrUnsafePath
	}
	current := string(filepath.Separator)
	insideSharedSticky := false
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect %q: %w", current, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("component %q: %w", current, ErrUnsafePath)
		}
		if insideSharedSticky && stat.Uid != uint32(os.Geteuid()) {
			return fmt.Errorf("component %q below shared sticky directory: %w", current, ErrUnsafePath)
		}
		if stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) {
			return fmt.Errorf("component %q ownership: %w", current, ErrUnsafePath)
		}
		sharedWritable := info.Mode().Perm()&0o022 != 0
		sticky := info.Mode()&os.ModeSticky != 0
		if sharedWritable && !sticky {
			return fmt.Errorf("component %q is shared-writable: %w", current, ErrUnsafePath)
		}
		insideSharedSticky = insideSharedSticky || sharedWritable && sticky
	}
	return nil
}

func ensureDirectory(path string, mode os.FileMode) error {
	if err := validatePathComponents(path); err != nil {
		return err
	}
	current := string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, mode); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create directory %q: %w", current, err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory %q: %w", current, errors.Join(err, ErrUnsafePath))
		}
	}
	if err := validatePathComponents(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect directory %q: %w", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("directory %q ownership: %w", path, ErrUnsafePath)
	}
	return nil
}

func validateOwnedDirectory(path string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect owned directory %q: %w", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != mode || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("owned directory %q: %w", path, ErrUnsafePath)
	}
	return nil
}

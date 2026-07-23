//go:build linux || darwin

package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type installationLock struct {
	file *os.File
	path string
}

func acquireInstallationLock(ctx context.Context, paths installPaths, callbacks hooks) (*installationLock, error) {
	if callbacks.beforeLock != nil {
		callbacks.beforeLock()
	}
	if err := ensureDirectory(paths.dataHome, 0o700); err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		file, err := openLockFile(paths.lock)
		if err != nil {
			return nil, err
		}
		if callbacks.afterLockOpen != nil {
			callbacks.afterLockOpen(attempt)
		}
		if err := lockInstallationFile(ctx, file); err != nil {
			return nil, errors.Join(fmt.Errorf("lock installation: %w", err), file.Close())
		}
		matches, err := lockMatchesNamedPath(file, paths.lock)
		if err != nil {
			return nil, errors.Join(err, unix.Flock(int(file.Fd()), unix.LOCK_UN), file.Close())
		}
		if !matches {
			if err := errors.Join(unix.Flock(int(file.Fd()), unix.LOCK_UN), file.Close()); err != nil {
				return nil, fmt.Errorf("release stale installation lock: %w", err)
			}
			continue
		}
		if callbacks.afterLock != nil {
			callbacks.afterLock()
		}
		return &installationLock{file: file, path: paths.lock}, nil
	}
}

func lockInstallationFile(ctx context.Context, file *os.File) error {
	const retryInterval = 25 * time.Millisecond
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return err
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func openLockFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if errors.Is(err, unix.ELOOP) {
		return nil, fmt.Errorf("installation lock is a symlink: %w", ErrUnsafePath)
	}
	if err != nil {
		return nil, fmt.Errorf("open installation lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		return nil, errors.Join(ErrUnsafePath, unix.Close(fd))
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, errors.Join(fmt.Errorf("inspect installation lock: %w", err), file.Close())
	}
	if !safeLockStat(stat) {
		return nil, errors.Join(ErrUnsafePath, file.Close())
	}
	return file, nil
}

func lockMatchesNamedPath(file *os.File, path string) (bool, error) {
	var descriptor unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &descriptor); err != nil {
		return false, fmt.Errorf("inspect locked descriptor: %w", err)
	}
	if !safeLockStat(descriptor) {
		return false, nil
	}
	var named unix.Stat_t
	if err := unix.Fstatat(unix.AT_FDCWD, path, &named, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect named installation lock: %w", err)
	}
	if named.Mode&unix.S_IFMT != unix.S_IFREG {
		return false, fmt.Errorf("named installation lock changed type: %w", ErrUnsafePath)
	}
	if !safeLockStat(named) {
		return false, nil
	}
	return descriptor.Dev == named.Dev && descriptor.Ino == named.Ino, nil
}

func safeLockStat(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0o777 == 0o600 &&
		stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1
}

func (lock *installationLock) unlinkNamed() error {
	matches, err := lockMatchesNamedPath(lock.file, lock.path)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("installation lock namespace changed: %w", ErrForeignPath)
	}
	if err := unix.Unlink(lock.path); err != nil {
		return fmt.Errorf("unlink installation lock: %w", err)
	}
	return syncDirectory(filepath.Dir(lock.path))
}

func (lock *installationLock) close() error {
	return errors.Join(unix.Flock(int(lock.file.Fd()), unix.LOCK_UN), lock.file.Close())
}

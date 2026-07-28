//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func createSyncedFile(directory *os.File, name string) error {
	file, err := createPrivateFileAt(directory, name)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync %s: %w", name, err), file.Close())
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	return nil
}

func entryExists(directory *os.File, name string) (bool, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(fileFD(directory), name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect entry %q: %w", name, err)
	}
	return true, nil
}

func removeDirectoryContents(directory *os.File) error {
	duplicatedFD, err := duplicateCloseOnExec(directory)
	if err != nil {
		return fmt.Errorf("duplicate directory descriptor: %w", err)
	}
	reader, err := fileFromFD(duplicatedFD, "remove-directory")
	if err != nil {
		return err
	}
	entries, readErr := reader.ReadDir(-1)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return errors.Join(fmt.Errorf("read directory: %w", readErr), closeErr)
	}
	for _, entry := range entries {
		var stat unix.Stat_t
		if err := unix.Fstatat(fileFD(directory), entry.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return fmt.Errorf("inspect removal entry %q: %w", entry.Name(), err)
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			return fmt.Errorf("remove nested directory %q: %w", entry.Name(), ErrUnsafeFilesystem)
		}
	}
	for _, entry := range entries {
		if err := unix.Unlinkat(fileFD(directory), entry.Name(), 0); err != nil {
			return fmt.Errorf("remove entry %q: %w", entry.Name(), err)
		}
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync emptied directory: %w", err)
	}
	return nil
}

func duplicateCloseOnExec(file *os.File) (int, error) {
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("duplicate descriptor: %w", err)
	}
	return fd, nil
}

func removeDirectory(parent *os.File, name string) error {
	if err := unix.Unlinkat(fileFD(parent), name, unix.AT_REMOVEDIR); err != nil {
		return fmt.Errorf("remove directory %q: %w", name, err)
	}
	if err := parent.Sync(); err != nil {
		return fmt.Errorf("sync removed directory %q: %w", name, err)
	}
	return nil
}

func lockFile(file *os.File, nonblocking bool) error {
	operation := unix.LOCK_EX
	if nonblocking {
		operation |= unix.LOCK_NB
	}
	if err := unix.Flock(fileFD(file), operation); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return ErrRunnerActive
		}
		return fmt.Errorf("lock file: %w", err)
	}
	return nil
}

func lockStateFile(ctx context.Context, file *os.File, timeout time.Duration, poll time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		err := unix.Flock(fileFD(file), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return fmt.Errorf("lock state file: %w", err)
		}
		retry := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			if !retry.Stop() {
				<-retry.C
			}
			return ctx.Err()
		case <-deadline.C:
			if !retry.Stop() {
				<-retry.C
			}
			return ErrStateLockTimeout
		case <-retry.C:
		}
	}
}

func lockStateFileBlocking(file *os.File) error {
	if err := unix.Flock(fileFD(file), unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock state file: %w", err)
	}
	return nil
}

func unlockFile(file *os.File) error {
	if err := unix.Flock(fileFD(file), unix.LOCK_UN); err != nil {
		return fmt.Errorf("unlock file: %w", err)
	}
	return nil
}

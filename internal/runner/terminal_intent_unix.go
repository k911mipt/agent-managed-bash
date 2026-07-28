//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"golang.org/x/sys/unix"
)

const terminalIntentName = "terminal.pending"

func (store *Store) createTerminalIntent(jobID generated.JobID) (returnErr error) {
	if !validJobID(jobID) {
		return ErrInvalidJobID
	}
	directory, err := openDirectoryAt(store.jobs, string(jobID), true)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, directory.Close()) }()
	if err := createSyncedFile(directory, terminalIntentName); err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync terminal intent: %w", err)
	}
	return nil
}

func (store *Store) clearTerminalIntent(jobID generated.JobID) (returnErr error) {
	if !validJobID(jobID) {
		return ErrInvalidJobID
	}
	directory, err := openDirectoryAt(store.jobs, string(jobID), true)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, directory.Close()) }()
	return removeTerminalIntent(directory)
}

func removeTerminalIntent(directory *os.File) error {
	exists, err := terminalIntentExists(directory)
	if err != nil || !exists {
		return err
	}
	if err := unix.Unlinkat(fileFD(directory), terminalIntentName, 0); err != nil {
		return fmt.Errorf("remove terminal intent: %w", err)
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync removed terminal intent: %w", err)
	}
	return nil
}

func (store *Store) waitForTerminalIntent(ctx context.Context, jobID generated.JobID, deadline time.Time) error {
	if !validJobID(jobID) {
		return ErrInvalidJobID
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		directory, err := openDirectoryAt(store.jobs, string(jobID), true)
		if err != nil {
			return err
		}
		intentExists, inspectErr := terminalIntentExists(directory)
		closeErr := directory.Close()
		if inspectErr != nil || closeErr != nil {
			return errors.Join(inspectErr, closeErr)
		}
		if !intentExists {
			return nil
		}
		if time.Until(deadline) <= 0 {
			return ErrStateLockTimeout
		}
		runnerActive, err := store.reconcileRunnerState(ctx, jobID, deadline)
		if err != nil {
			return err
		}
		if !runnerActive {
			continue
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ErrStateLockTimeout
		}
		timer := time.NewTimer(min(store.lockPoll, remaining))
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

func terminalIntentExists(directory *os.File) (bool, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(fileFD(directory), terminalIntentName, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect terminal intent: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(os.Geteuid()) ||
		stat.Mode&0o777 != privateFileMode || stat.Nlink != 1 {
		return false, ErrUnsafeFilesystem
	}
	return true, nil
}

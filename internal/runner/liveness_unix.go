//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"golang.org/x/sys/unix"
)

const recoveryLockName = "recovery.lock"

func (store *Store) reconcileRunnerState(
	ctx context.Context,
	jobID generated.JobID,
	deadline time.Time,
) (bool, error) {
	if !validJobID(jobID) {
		return false, ErrInvalidJobID
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	directory, err := openDirectoryAt(store.jobs, string(jobID), true)
	if err != nil {
		return false, err
	}
	recoveryLock, err := openRecoveryLockAt(directory)
	if err != nil {
		return false, errors.Join(err, directory.Close())
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false, errors.Join(ErrStateLockTimeout, recoveryLock.Close(), directory.Close())
	}
	if err := lockStateFile(ctx, recoveryLock, remaining, store.lockPoll); err != nil {
		return false, errors.Join(err, recoveryLock.Close(), directory.Close())
	}
	closeRecovery := func() error {
		return errors.Join(unlockFile(recoveryLock), recoveryLock.Close(), directory.Close())
	}
	if store.afterRecoveryLock != nil {
		store.afterRecoveryLock()
	}
	stored, err := store.readState(directory, jobID)
	if err != nil {
		return false, errors.Join(err, closeRecovery())
	}
	intentExists, err := terminalIntentExists(directory)
	if err != nil {
		return false, errors.Join(err, closeRecovery())
	}
	if stored.Job.Status != generated.JobStatusRunning && !intentExists {
		return false, closeRecovery()
	}
	runnerLock, err := openPrivateFileAt(directory, "runner.lock", unix.O_RDWR)
	if err != nil {
		return false, errors.Join(err, closeRecovery())
	}
	if err := lockFile(runnerLock, true); err != nil {
		if errors.Is(err, ErrRunnerActive) {
			return true, errors.Join(runnerLock.Close(), closeRecovery())
		}
		return false, errors.Join(err, runnerLock.Close(), closeRecovery())
	}
	closeRunner := func() error {
		return errors.Join(unlockFile(runnerLock), runnerLock.Close(), closeRecovery())
	}
	if stored.Job.Status != generated.JobStatusRunning {
		return false, errors.Join(removeTerminalIntent(directory), closeRunner())
	}
	if !intentExists {
		if err := createSyncedFile(directory, terminalIntentName); err != nil {
			return false, errors.Join(err, closeRunner())
		}
		if err := directory.Sync(); err != nil {
			return false, errors.Join(err, closeRunner())
		}
	}
	stateLock, err := openPrivateFileAt(directory, "state.lock", unix.O_RDWR)
	if err != nil {
		return false, errors.Join(err, closeRunner())
	}
	remaining = time.Until(deadline)
	if remaining <= 0 {
		return false, errors.Join(ErrStateLockTimeout, stateLock.Close(), closeRunner())
	}
	if err := lockStateFile(ctx, stateLock, remaining, store.lockPoll); err != nil {
		return false, errors.Join(err, stateLock.Close(), closeRunner())
	}
	stored, err = store.readState(directory, jobID)
	if err != nil {
		return false, errors.Join(err, unlockFile(stateLock), stateLock.Close(), closeRunner())
	}
	job := &lockedJob{dir: directory, stateLock: stateLock, state: stored}
	operationErr := store.publishRunnerLostLocked(job, jobID)
	operationErr = errors.Join(operationErr, removeTerminalIntent(job.dir))
	return false, errors.Join(operationErr, unlockFile(stateLock), stateLock.Close(), closeRunner())
}

func openRecoveryLockAt(directory *os.File) (*os.File, error) {
	for {
		file, err := openPrivateFileAt(directory, recoveryLockName, unix.O_RDWR)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, ErrJobNotFound) {
			return nil, err
		}
		file, err = createPrivateFileAt(directory, recoveryLockName)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := file.Sync(); err != nil {
			return nil, errors.Join(err, file.Close())
		}
		if err := directory.Sync(); err != nil {
			return nil, errors.Join(err, file.Close())
		}
		return file, nil
	}
}

func (store *Store) publishRunnerLostLocked(job *lockedJob, jobID generated.JobID) error {
	if job.state.Job.Status != generated.JobStatusRunning {
		return nil
	}
	diagnostic := "runner liveness lock disappeared; process group cleanup skipped because ownership cannot be pinned"
	finished := generated.TimestampUnixMs(time.Now().UnixMilli())
	finished = max(finished, job.state.Job.StartedAtUnixMs)
	next := job.state
	next.Job.Status = generated.JobStatusRunnerLost
	next.Job.FinishedAtUnixMs = &finished
	next.Result = &generated.ProcessResult{
		Status: generated.TerminalStatusRunnerLost, FinishedAtUnixMs: finished,
		CapturedBytes: next.Job.CapturedBytes, Diagnostic: &diagnostic,
	}
	if err := store.publishStateLocked(job, jobID, next); err != nil {
		return err
	}
	job.state = next
	return nil
}

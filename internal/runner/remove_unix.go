//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"golang.org/x/sys/unix"
)

func (store *Store) removeTerminal(ctx context.Context, jobID generated.JobID) error {
	snapshot, err := store.openSnapshotJob(jobID)
	if err != nil {
		return err
	}
	authorization := store.contracts.Policy().AuthorizeMutation(state.AccessContext{
		JobWorkspace: snapshot.state.Job.WorkspacePath, RequestWorkspace: store.workspace,
		OwnerSession: snapshot.state.Job.OwnerSessionID, ActorSession: store.sessionID,
	})
	running := snapshot.state.Job.Status == generated.JobStatusRunning
	if closeErr := snapshot.close(); closeErr != nil {
		return closeErr
	}
	if !authorization.Allowed {
		return decisionError(authorization.Code)
	}
	if running {
		runnerActive, err := store.reconcileRunnerState(ctx, jobID, time.Now().Add(store.lockTimeout))
		if err != nil {
			return err
		}
		if runnerActive {
			return ErrActiveJob
		}
	}
	directory, err := openDirectoryAt(store.jobs, string(jobID), true)
	if err != nil {
		return err
	}
	runnerLock, err := openPrivateFileAt(directory, "runner.lock", unix.O_RDWR)
	if err != nil {
		return errors.Join(err, directory.Close())
	}
	if err := lockFile(runnerLock, true); err != nil {
		return errors.Join(err, runnerLock.Close(), directory.Close())
	}
	if err := directory.Close(); err != nil {
		return errors.Join(err, unlockFile(runnerLock), runnerLock.Close())
	}
	job, err := store.openLockedJobWith(jobID, func(file *os.File) error {
		return lockStateFile(ctx, file, store.lockTimeout, store.lockPoll)
	})
	if err != nil {
		return errors.Join(err, unlockFile(runnerLock), runnerLock.Close())
	}
	authorization = store.contracts.Policy().AuthorizeMutation(state.AccessContext{
		JobWorkspace: job.state.Job.WorkspacePath, RequestWorkspace: store.workspace,
		OwnerSession: job.state.Job.OwnerSessionID, ActorSession: store.sessionID,
	})
	var operationErr error
	contentsRemoved := false
	if !authorization.Allowed {
		operationErr = decisionError(authorization.Code)
	} else if decision := store.contracts.Policy().AuthorizeRemoval(job.state.Job.Status); !decision.Allowed {
		if decision.Code == state.CodeActiveJob {
			operationErr = ErrActiveJob
		} else {
			operationErr = ErrCorruptState
		}
	} else {
		removal, removeErr := removeDirectoryContents(job.dir, store.syncDirectory)
		operationErr = removeErr
		contentsRemoved = removal.emptied
	}
	stateCloseErr := errors.Join(unlockFile(job.stateLock), job.stateLock.Close(), store.closeJob(job.dir))
	runnerCloseErr := errors.Join(unlockFile(runnerLock), runnerLock.Close())
	cleanupErr := errors.Join(operationErr, stateCloseErr, runnerCloseErr)
	if !contentsRemoved {
		return cleanupErr
	}
	return errors.Join(cleanupErr, removeDirectory(store.jobs, string(jobID)))
}

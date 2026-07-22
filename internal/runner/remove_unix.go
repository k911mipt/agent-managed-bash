//go:build linux || darwin

package runner

import (
	"context"
	"errors"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"golang.org/x/sys/unix"
)

func (store *Store) removeTerminal(ctx context.Context, jobID generated.JobID) (err error) {
	job, err := store.openLockedJobContext(ctx, jobID)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, job.close())
	}()
	authorization := store.contracts.Policy().AuthorizeMutation(state.AccessContext{
		JobWorkspace: job.state.Job.WorkspacePath, RequestWorkspace: store.workspace,
		OwnerSession: job.state.Job.OwnerSessionID, ActorSession: store.sessionID,
	})
	if !authorization.Allowed {
		return decisionError(authorization.Code)
	}
	if err := store.reconcileRunnerLostLocked(job, jobID); err != nil {
		return err
	}
	decision := store.contracts.Policy().AuthorizeRemoval(job.state.Job.Status)
	if !decision.Allowed {
		if decision.Code == state.CodeActiveJob {
			return ErrActiveJob
		}
		return ErrCorruptState
	}
	runnerLock, err := openPrivateFileAt(job.dir, "runner.lock", unix.O_RDWR)
	if err != nil {
		return err
	}
	if err := lockFile(runnerLock, true); err != nil {
		return errors.Join(err, runnerLock.Close())
	}
	defer func() {
		err = errors.Join(err, unlockFile(runnerLock), runnerLock.Close())
	}()
	if err := removeDirectoryContents(job.dir); err != nil {
		return err
	}
	return removeDirectory(store.jobs, string(jobID))
}

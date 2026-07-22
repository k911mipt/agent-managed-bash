//go:build linux || darwin

package runner

import (
	"errors"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"golang.org/x/sys/unix"
)

func (store *Store) reconcileRunnerLostLocked(job *lockedJob, jobID generated.JobID) (returnErr error) {
	if job.state.Job.Status != generated.JobStatusRunning {
		return nil
	}
	runnerLock, err := openPrivateFileAt(job.dir, "runner.lock", unix.O_RDWR)
	if err != nil {
		return err
	}
	if err := lockFile(runnerLock, true); err != nil {
		closeErr := runnerLock.Close()
		if errors.Is(err, ErrRunnerActive) {
			return closeErr
		}
		return errors.Join(err, closeErr)
	}
	defer func() { returnErr = errors.Join(returnErr, unlockFile(runnerLock), runnerLock.Close()) }()
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

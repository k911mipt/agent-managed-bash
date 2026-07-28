//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"golang.org/x/sys/unix"
)

func (store *Store) Load(jobID generated.JobID) (snapshot Snapshot, err error) {
	job, err := store.openSnapshotJob(jobID)
	return store.loadSnapshotJob(job, err)
}

func (store *Store) loadContext(ctx context.Context, jobID generated.JobID) (snapshot Snapshot, err error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	job, err := store.openSnapshotJob(jobID)
	return store.loadSnapshotJob(job, err)
}

func (store *Store) loadSnapshotJob(job *snapshotJob, openErr error) (snapshot Snapshot, err error) {
	if openErr != nil {
		return Snapshot{}, openErr
	}
	defer func() {
		err = errors.Join(err, job.close())
	}()
	runtime, err := readRuntime(job.dir)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{State: job.state, Runtime: runtime}, nil
}

func (store *Store) publishState(jobID generated.JobID, next generated.PersistedJobState) (err error) {
	job, err := store.openLockedJob(jobID)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, job.close())
	}()
	if next.Job.Status != job.state.Job.Status {
		return ErrInvalidStateUpdate
	}
	return store.publishStateLocked(job, jobID, next)
}

func (store *Store) publishTerminal(
	jobID generated.JobID,
	next generated.PersistedJobState,
	lease *runnerLease,
) (err error) {
	return store.publishTerminalState(jobID, lease, func(generated.PersistedJobState) (generated.PersistedJobState, error) {
		return next, nil
	})
}

func (store *Store) publishStateLocked(
	job *lockedJob,
	jobID generated.JobID,
	next generated.PersistedJobState,
) error {
	if !sameImmutableStateFields(job.state, next) || next.Job.JobID != jobID ||
		next.Job.CapturedBytes != job.state.Job.CapturedBytes ||
		!preservesPublishedFields(job.state, next) {
		return ErrInvalidStateUpdate
	}
	output, err := openPrivateFileAt(job.dir, "output.log", unix.O_RDONLY)
	if err != nil {
		return err
	}
	info, statErr := output.Stat()
	closeErr := output.Close()
	if statErr != nil || closeErr != nil {
		return errors.Join(fmt.Errorf("inspect output: %w", statErr), closeErr)
	}
	if info.Size() < int64(next.Job.CapturedBytes) {
		return ErrInvalidStateUpdate
	}
	raw, err := store.encodeState(next)
	if err != nil {
		return errors.Join(ErrInvalidStateUpdate, err)
	}
	return writeAtomicFile(job.dir, "state.json", raw)
}

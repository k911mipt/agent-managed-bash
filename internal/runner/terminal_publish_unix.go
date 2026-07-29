//go:build linux || darwin

package runner

import (
	"errors"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

type terminalStateBuilder func(generated.PersistedJobState) (generated.PersistedJobState, error)

func (store *Store) publishTerminalState(
	jobID generated.JobID,
	lease *runnerLease,
	build terminalStateBuilder,
) error {
	if lease == nil {
		return ErrInvalidStateUpdate
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.file == nil || lease.jobID != jobID || lease.store != store {
		return ErrInvalidStateUpdate
	}
	directory, err := openDirectoryAt(store.jobs, string(jobID), true)
	if err != nil {
		return err
	}
	recoveryLock, err := openRecoveryLockAt(directory)
	if err != nil {
		return errors.Join(err, directory.Close())
	}
	if store.beforeTerminalRecoveryLock != nil {
		store.beforeTerminalRecoveryLock()
	}
	if err := lockStateFileBlocking(recoveryLock); err != nil {
		return errors.Join(err, recoveryLock.Close(), directory.Close())
	}
	closeRecovery := func() error {
		return errors.Join(unlockFile(recoveryLock), recoveryLock.Close(), directory.Close())
	}
	if err := store.createTerminalIntent(jobID); err != nil {
		return errors.Join(err, closeRecovery())
	}
	if store.afterTerminalIntent != nil {
		store.afterTerminalIntent()
	}
	job, err := store.openLockedJobWith(jobID, lockStateFileBlocking)
	if err != nil {
		return errors.Join(err, store.clearTerminalIntent(jobID), closeRecovery())
	}
	next, operationErr := build(job.state)
	if operationErr == nil && (next.Job.Status == job.state.Job.Status ||
		!transitionAllowed(store.contracts.Policy(), job.state, next)) {
		operationErr = ErrInvalidStateUpdate
	}
	if operationErr == nil {
		operationErr = store.publishStateLocked(job, jobID, next)
	}
	intentErr := removeTerminalIntent(job.dir)
	stateCloseErr := job.close()
	if err := errors.Join(operationErr, intentErr, stateCloseErr, closeRecovery()); err != nil {
		return err
	}
	return lease.releaseLocked()
}

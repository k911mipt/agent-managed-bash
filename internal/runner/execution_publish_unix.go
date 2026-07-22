//go:build linux || darwin

package runner

import (
	"errors"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

func (store *Store) publishExecutionTerminal(
	jobID generated.JobID,
	outcome executionOutcome,
	lease *runnerLease,
) (returnErr error) {
	if lease == nil {
		return ErrInvalidStateUpdate
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.file == nil || lease.jobID != jobID || lease.store != store {
		return ErrInvalidStateUpdate
	}
	job, err := store.openLockedJob(jobID)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, job.close()) }()
	if job.state.Cancellation != nil {
		outcome.cause = causeCancelled
	}
	terminal, err := terminalState(job.state, outcome)
	if err != nil {
		return err
	}
	if !transitionAllowed(store.contracts.Policy(), job.state, terminal) {
		return ErrInvalidStateUpdate
	}
	if err := store.publishStateLocked(job, jobID, terminal); err != nil {
		return err
	}
	return lease.releaseLocked()
}

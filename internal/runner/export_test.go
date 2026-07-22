//go:build linux || darwin

package runner

import (
	"context"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

type PendingJob = pendingJob
type RunnerLease = runnerLease
type OutputAppend = outputAppend

func (store *Store) Prepare(state generated.PersistedJobState, runtime RuntimeMetadata) (*PendingJob, error) {
	return store.prepare(state, runtime)
}

func (store *Store) PublishState(jobID generated.JobID, state generated.PersistedJobState) error {
	return store.publishState(jobID, state)
}

func (store *Store) PublishTerminal(jobID generated.JobID, state generated.PersistedJobState, lease *RunnerLease) error {
	return store.publishTerminal(jobID, state, lease)
}

func (store *Store) AppendOutput(jobID generated.JobID, output []byte) (OutputAppend, error) {
	return store.appendOutput(jobID, output)
}

func (store *Store) RemoveTerminal(jobID generated.JobID) error {
	return store.removeTerminal(context.Background(), jobID)
}

func (pending *pendingJob) Commit() (*runnerLease, error) {
	return pending.commit()
}

func (pending *pendingJob) Abort() error {
	return pending.abort()
}

func (lease *runnerLease) Release() error {
	return lease.release()
}

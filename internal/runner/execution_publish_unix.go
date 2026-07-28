//go:build linux || darwin

package runner

import "github.com/k911mipt/agent-managed-bash/internal/protocol/generated"

func (store *Store) publishExecutionTerminal(
	jobID generated.JobID,
	outcome executionOutcome,
	lease *runnerLease,
) error {
	return store.publishTerminalState(jobID, lease, func(current generated.PersistedJobState) (generated.PersistedJobState, error) {
		if current.Cancellation != nil {
			outcome.cause = causeCancelled
		}
		return terminalState(current, outcome)
	})
}

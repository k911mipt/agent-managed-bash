//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

func (store *Store) cancel(ctx context.Context, jobID generated.JobID) (result generated.CancellationResult, err error) {
	job, err := store.openLockedJobContext(ctx, jobID)
	if err != nil {
		return generated.CancellationResult{}, err
	}
	defer func() { err = errors.Join(err, job.close()) }()
	authorization := store.contracts.Policy().AuthorizeMutation(state.AccessContext{
		JobWorkspace: job.state.Job.WorkspacePath, RequestWorkspace: store.workspace,
		OwnerSession: job.state.Job.OwnerSessionID, ActorSession: store.sessionID,
	})
	if !authorization.Allowed {
		return generated.CancellationResult{}, decisionError(authorization.Code)
	}
	if err := store.reconcileRunnerLostLocked(job, jobID); err != nil {
		return generated.CancellationResult{}, err
	}
	decision := store.contracts.Policy().EvaluateCancellation(state.CancellationContext{
		Status: job.state.Job.Status, AlreadyRequested: job.state.Cancellation != nil,
	})
	if !decision.Allowed {
		return generated.CancellationResult{}, decisionError(decision.Code)
	}
	if decision.PersistRequest {
		requestedAt := generated.TimestampUnixMs(time.Now().UnixMilli())
		requestedAt = max(requestedAt, job.state.Job.CreatedAtUnixMs)
		next := job.state
		next.Cancellation = &generated.CancellationMetadata{
			Requested: true, RequestedAtUnixMs: requestedAt, RequestedBySessionID: store.sessionID,
		}
		if err := store.publishStateLocked(job, jobID, next); err != nil {
			return generated.CancellationResult{}, err
		}
		job.state = next
	}
	return cancellationResult(job.state, decision.Code), nil
}

func cancellationResult(persisted generated.PersistedJobState, code state.Code) generated.CancellationResult {
	outcome := generated.CancellationOutcomeAlreadyTerminal
	if code == state.CodeCancellationRequested {
		outcome = generated.CancellationOutcomeRequested
	} else if code == state.CodeCancellationIdempotent {
		outcome = generated.CancellationOutcomeAlreadyRequested
	}
	return generated.CancellationResult{
		JobID: persisted.Job.JobID, Status: persisted.Job.Status,
		Outcome: outcome, Cancellation: persisted.Cancellation,
	}
}

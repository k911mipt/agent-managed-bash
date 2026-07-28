//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

func (manager *Manager) PrepareWait(ctx context.Context, request WaitRequest) (prepared *PreparedWait, returnErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timeout := request.Timeout
	if timeout == 0 {
		timeout = time.Duration(manager.contracts.Policy().WaitTimeout()) * time.Millisecond
	}
	idle := request.IdleTimeout
	if idle == 0 {
		idle = time.Duration(manager.contracts.Policy().IdleCheckpoint()) * time.Millisecond
	}
	if timeout < time.Millisecond || idle < time.Millisecond {
		return nil, ErrInvalidStateUpdate
	}
	store, err := manager.openStore(request.Invocation)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, store.Close()) }()
	started := time.Now()
	absoluteDeadline := started.Add(timeout)
	waitContext, cancelWait := context.WithDeadline(ctx, absoluteDeadline)
	defer cancelWait()
	idleDeadline := started.Add(idle)
	cursor := request.CursorBytes
	lastCaptured := generated.ByteCursor(-1)
	for {
		metadata, resolved, err := store.waitMetadata(waitContext, request.JobID, request.Invocation.SessionID(), cursor)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				observation, snapshotCursor, snapshotErr := store.waitSnapshot(request.JobID, request.Invocation.SessionID(), cursor)
				if snapshotErr != nil {
					return nil, snapshotErr
				}
				return newPreparedWait(manager, request, observation, snapshotCursor, time.Now()), nil
			}
			return nil, err
		}
		if cursor == nil {
			cursor = &resolved
		}
		now := time.Now()
		if metadata.Job.CapturedBytes > lastCaptured {
			lastCaptured = metadata.Job.CapturedBytes
			idleDeadline = now.Add(idle)
		}
		if metadata.Job.Status != generated.JobStatusRunning || !now.Before(absoluteDeadline) ||
			!now.Before(idleDeadline) {
			if manager.beforeWaitOutput != nil {
				manager.beforeWaitOutput()
			}
			observation, err := store.waitOutput(waitContext, request.JobID, resolved)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
					observation, resolved, err = store.waitSnapshot(request.JobID, request.Invocation.SessionID(), cursor)
				}
			}
			if err != nil {
				return nil, err
			}
			returnedAt := time.Now()
			if observation.Observation.Job.Status == generated.JobStatusRunning && returnedAt.Before(absoluteDeadline) &&
				observation.Observation.Job.CapturedBytes > lastCaptured {
				lastCaptured = observation.Observation.Job.CapturedBytes
				idleDeadline = returnedAt.Add(idle)
				continue
			}
			return newPreparedWait(manager, request, observation, resolved, returnedAt), nil
		}
		pause := min(manager.config.PollInterval, time.Until(absoluteDeadline), time.Until(idleDeadline))
		timer := time.NewTimer(max(pause, time.Millisecond))
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, ctx.Err()
		}
	}
}

func newPreparedWait(manager *Manager, request WaitRequest, observation generated.OutputObservation, _ generated.ByteCursor, now time.Time) *PreparedWait {
	updatedAt := generated.TimestampUnixMs(now.UnixMilli())
	updatedAt = max(updatedAt, observation.Observation.Job.CreatedAtUnixMs)
	if finished := observation.Observation.Job.FinishedAtUnixMs; finished != nil {
		updatedAt = min(updatedAt, *finished)
	}
	return &PreparedWait{
		Observation: observation, manager: manager, invocation: request.Invocation,
		jobID: request.JobID, cursor: observation.Output.NextCursorBytes, updatedAt: updatedAt,
		output: observation.Output,
	}
}

func (store *Store) waitSnapshot(jobID generated.JobID, sessionID generated.SessionID, explicit *generated.ByteCursor) (generated.OutputObservation, generated.ByteCursor, error) {
	if !validJobID(jobID) {
		return generated.OutputObservation{}, 0, ErrInvalidJobID
	}
	directory, err := openDirectoryAt(store.jobs, string(jobID), true)
	if err != nil {
		return generated.OutputObservation{}, 0, err
	}
	defer directory.Close()
	raw, err := readPrivateFileAt(directory, "state.json", maximumStateBytes)
	if err != nil {
		return generated.OutputObservation{}, 0, err
	}
	persisted, decision := store.contracts.StateValidator().ValidateStored(raw, store.workspace)
	if !decision.Allowed || persisted.Job.JobID != jobID {
		return generated.OutputObservation{}, 0, ErrCorruptState
	}
	if decision := store.contracts.Policy().AuthorizeRead(state.AccessContext{
		JobWorkspace: persisted.Job.WorkspacePath, RequestWorkspace: store.workspace,
		OwnerSession: persisted.Job.OwnerSessionID, ActorSession: store.sessionID,
	}); !decision.Allowed {
		return generated.OutputObservation{}, 0, decisionError(decision.Code)
	}
	var existing *generated.ObserverCursor
	for index := range persisted.Observers {
		if persisted.Observers[index].SessionID == sessionID {
			existing = &persisted.Observers[index]
			break
		}
	}
	resolved := store.contracts.Policy().ResolveWaitCursor(state.WaitCursorContext{Explicit: explicit, Observer: existing})
	payload := generated.OutputPayload{JobID: jobID, StartCursorBytes: &resolved}
	observation, err := store.outputLocked(&lockedJob{dir: directory, state: persisted}, payload)
	return observation, resolved, err
}

func (store *Store) waitMetadata(
	ctx context.Context,
	jobID generated.JobID,
	sessionID generated.SessionID,
	explicit *generated.ByteCursor,
) (observation generated.JobObservation, cursor generated.ByteCursor, err error) {
	job, err := store.openObservedJob(ctx, jobID)
	if err != nil {
		return generated.JobObservation{}, 0, err
	}
	defer func() { err = errors.Join(err, job.close()) }()
	var existing *generated.ObserverCursor
	for index := range job.state.Observers {
		if job.state.Observers[index].SessionID == sessionID {
			existing = &job.state.Observers[index]
			break
		}
	}
	resolved := store.contracts.Policy().ResolveWaitCursor(state.WaitCursorContext{Explicit: explicit, Observer: existing})
	decision := store.contracts.Policy().ValidateCursor(state.CursorContext{
		Cursor: int64(resolved), Captured: int64(job.state.Job.CapturedBytes),
	})
	if !decision.Allowed {
		return generated.JobObservation{}, 0, decisionError(decision.Code)
	}
	return observationFromState(job.state), resolved, nil
}

func (store *Store) waitOutput(ctx context.Context, jobID generated.JobID, cursor generated.ByteCursor) (observation generated.OutputObservation, err error) {
	job, err := store.openObservedJob(ctx, jobID)
	if err != nil {
		return generated.OutputObservation{}, err
	}
	defer func() { err = errors.Join(err, job.close()) }()
	payload := generated.OutputPayload{JobID: jobID, StartCursorBytes: &cursor}
	return store.outputLocked(job, payload)
}

func (prepared *PreparedWait) commit(ctx context.Context) error {
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.committed {
		return nil
	}
	store, err := prepared.manager.openStore(prepared.invocation)
	if err != nil {
		return err
	}
	commitErr := store.commitWait(ctx, prepared)
	closeErr := store.Close()
	if err := errors.Join(commitErr, closeErr); err != nil {
		return err
	}
	prepared.committed = true
	return nil
}

func (store *Store) commitWait(ctx context.Context, prepared *PreparedWait) (returnErr error) {
	job, err := store.openObservedJob(ctx, prepared.jobID)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, job.close()) }()
	index := -1
	current := generated.ObserverCursor{
		SessionID: store.sessionID, UpdatedAtUnixMs: job.state.Job.CreatedAtUnixMs,
	}
	for observerIndex := range job.state.Observers {
		if job.state.Observers[observerIndex].SessionID == store.sessionID {
			index = observerIndex
			current = job.state.Observers[observerIndex]
			break
		}
	}
	if index >= 0 && current.CursorBytes >= prepared.cursor {
		return nil
	}
	nextObserver, decision := store.contracts.Policy().ObserverAfter(state.ObserverAdvanceContext{
		Action: generated.ActionWait, Current: current, Output: prepared.output, UpdatedAtUnixMs: prepared.updatedAt,
	})
	if !decision.Allowed {
		return decisionError(decision.Code)
	}
	next := job.state
	if index < 0 {
		next.Observers = append(next.Observers, nextObserver)
	} else {
		next.Observers[index] = nextObserver
	}
	return store.publishStateLocked(job, prepared.jobID, next)
}

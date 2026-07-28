//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

func (store *Store) status(ctx context.Context, jobID generated.JobID) (observation generated.JobObservation, err error) {
	job, err := store.openObservedSnapshot(ctx, jobID)
	if err != nil {
		return generated.JobObservation{}, err
	}
	defer func() { err = errors.Join(err, job.close()) }()
	return observationFromState(job.state), nil
}

func (store *Store) output(ctx context.Context, request OutputRequest) (observation generated.OutputObservation, err error) {
	job, err := store.openObservedSnapshot(ctx, request.JobID)
	if err != nil {
		return generated.OutputObservation{}, err
	}
	defer func() { err = errors.Join(err, job.close()) }()
	payload := generated.OutputPayload{
		JobID: request.JobID, StartCursorBytes: request.StartCursorBytes, EndCursorBytes: request.EndCursorBytes,
	}
	return store.outputSnapshot(job, payload)
}

func (store *Store) openObservedSnapshot(ctx context.Context, jobID generated.JobID) (*snapshotJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	job, err := store.openAuthorizedSnapshot(jobID)
	if err != nil {
		return nil, err
	}
	if job.state.Job.Status != generated.JobStatusRunning {
		return job, nil
	}
	deadline := time.Now().Add(store.lockTimeout)
	if err := store.waitForTerminalIntent(ctx, jobID, deadline); err != nil {
		return nil, errors.Join(err, job.close())
	}
	if err := job.close(); err != nil {
		return nil, err
	}
	job, err = store.openAuthorizedSnapshot(jobID)
	if err != nil || job.state.Job.Status != generated.JobStatusRunning {
		return job, err
	}
	runnerActive, err := store.reconcileRunnerState(ctx, jobID, deadline)
	if err != nil || runnerActive {
		if err != nil {
			return nil, errors.Join(err, job.close())
		}
		return job, nil
	}
	if err := job.close(); err != nil {
		return nil, err
	}
	return store.openAuthorizedSnapshot(jobID)
}

func (store *Store) openAuthorizedSnapshot(jobID generated.JobID) (*snapshotJob, error) {
	job, err := store.openSnapshotJob(jobID)
	if err != nil {
		return nil, err
	}
	if decision := store.contracts.Policy().AuthorizeRead(state.AccessContext{
		JobWorkspace: job.state.Job.WorkspacePath, RequestWorkspace: store.workspace,
		OwnerSession: job.state.Job.OwnerSessionID, ActorSession: store.sessionID,
	}); !decision.Allowed {
		return nil, errors.Join(decisionError(decision.Code), job.close())
	}
	return job, nil
}

func (store *Store) outputSnapshot(job *snapshotJob, payload generated.OutputPayload) (generated.OutputObservation, error) {
	if store.beforeOutputRead != nil {
		store.beforeOutputRead()
	}
	raw, err := readOutputSnapshot(job)
	if err != nil {
		return generated.OutputObservation{}, err
	}
	resolved, decision := store.contracts.Policy().ResolveOutputRange(payload, int64(job.state.Job.CapturedBytes))
	if !decision.Allowed {
		return generated.OutputObservation{}, decisionError(decision.Code)
	}
	chunk, decision := store.contracts.Policy().Output(raw, state.OutputContext{
		Range: resolved, Terminal: job.state.Job.Status != generated.JobStatusRunning,
	})
	if !decision.Allowed {
		return generated.OutputObservation{}, decisionError(decision.Code)
	}
	return generated.OutputObservation{Observation: observationFromState(job.state), Output: chunk}, nil
}

func observationFromState(persisted generated.PersistedJobState) generated.JobObservation {
	return generated.JobObservation{Job: persisted.Job, ProcessResult: persisted.Result}
}

func (store *Store) list(ctx context.Context) (generated.ListResult, error) {
	duplicated, err := duplicateCloseOnExec(store.jobs)
	if err != nil {
		return generated.ListResult{}, err
	}
	reader, err := fileFromFD(duplicated, "list-jobs")
	if err != nil {
		return generated.ListResult{}, err
	}
	entries, readErr := reader.ReadDir(-1)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return generated.ListResult{}, errors.Join(readErr, closeErr)
	}
	if store.afterListEntries != nil {
		store.afterListEntries()
	}
	jobs := make([]generated.JobMetadata, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".starting-") {
			continue
		}
		if !entry.IsDir() || !validJobID(generated.JobID(entry.Name())) {
			return generated.ListResult{}, ErrUnsafeFilesystem
		}
		observation, err := store.status(ctx, generated.JobID(entry.Name()))
		if err != nil {
			if errors.Is(err, ErrJobNotFound) {
				continue
			}
			return generated.ListResult{}, err
		}
		jobs = append(jobs, observation.Job)
	}
	slices.SortFunc(jobs, func(left, right generated.JobMetadata) int {
		return strings.Compare(string(left.JobID), string(right.JobID))
	})
	return generated.ListResult{Jobs: jobs}, nil
}

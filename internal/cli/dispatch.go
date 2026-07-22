package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

type dispatchResult struct {
	response   any
	warning    error
	afterWrite func(context.Context) error
}

type runnerDispatch struct {
	manager    *runner.Manager
	invocation state.TrustedInvocation
}

func (application *Application) dispatch(
	ctx context.Context,
	request requestValue,
	invocation state.TrustedInvocation,
) (dispatchResult, *failure) {
	if request.action() == generated.ActionVersion {
		return dispatchResult{response: application.versionResponse()}, nil
	}
	manager, err := runner.New(application.config.Runner)
	if err != nil {
		return dispatchResult{}, failureFromError(request.action(), err)
	}
	runtime := runnerDispatch{manager: manager, invocation: invocation}
	switch value := request.(type) {
	case runRequest:
		return application.dispatchRun(ctx, runtime, value)
	case waitRequest:
		return application.dispatchWait(ctx, runtime, value)
	case statusRequest:
		result, err := manager.Status(ctx, runner.StatusRequest{Invocation: runtime.invocation, JobID: value.Payload.JobID})
		return operationResult(value.action(), generated.StatusResponse{Action: string(value.action()), Ok: true, Result: result, SchemaVersion: 1}, err)
	case outputRequest:
		result, err := manager.Output(ctx, runner.OutputRequest{
			Invocation: runtime.invocation, JobID: value.Payload.JobID,
			StartCursorBytes: value.Payload.StartCursorBytes, EndCursorBytes: value.Payload.EndCursorBytes,
		})
		return operationResult(value.action(), generated.OutputResponse{Action: string(value.action()), Ok: true, Result: result, SchemaVersion: 1}, err)
	case cancelRequest:
		result, err := manager.Cancel(ctx, runner.CancelRequest{Invocation: runtime.invocation, JobID: value.Payload.JobID})
		return operationResult(value.action(), generated.CancelResponse{Action: string(value.action()), Ok: true, Result: result, SchemaVersion: 1}, err)
	case removeRequest:
		result, err := manager.Remove(ctx, runner.RemoveRequest{Invocation: runtime.invocation, JobID: value.Payload.JobID})
		return operationResult(value.action(), generated.RemoveResponse{Action: string(value.action()), Ok: true, Result: result, SchemaVersion: 1}, err)
	case listRequest:
		result, err := manager.List(ctx, runner.ListRequest{Invocation: runtime.invocation})
		return operationResult(value.action(), generated.ListResponse{Action: string(value.action()), Ok: true, Result: result, SchemaVersion: 1}, err)
	case versionRequest:
		return dispatchResult{response: application.versionResponse()}, nil
	default:
		return dispatchResult{}, newFailure(request.action(), newProblem(generated.ErrorCodeInternal, fmt.Errorf("unhandled request type %T", request)))
	}
}

func (application *Application) dispatchRun(
	ctx context.Context,
	runtime runnerDispatch,
	request runRequest,
) (dispatchResult, *failure) {
	metadata, err := runtime.manager.Start(ctx, runner.StartRequest{
		Invocation: runtime.invocation, Command: request.Payload.Command,
		HardTimeout: durationFrom(request.Payload.HardTimeoutMs), OutputLimitBytes: intFrom(request.Payload.OutputLimitBytes),
	})
	return runResult(metadata, err)
}

func runResult(metadata generated.JobMetadata, err error) (dispatchResult, *failure) {
	if err == nil {
		return dispatchResult{response: generated.RunResponse{Action: "run", Ok: true, Result: metadata, SchemaVersion: 1}}, nil
	}
	var durabilityError *runner.CommitDurabilityError
	if errors.As(err, &durabilityError) {
		if metadata.JobID == "" || metadata.JobID != durabilityError.JobID {
			return dispatchResult{}, newFailure(generated.ActionRun, newProblem(generated.ErrorCodeInternal, fmt.Errorf("committed job metadata does not match durability error: %w", err)))
		}
		return dispatchResult{
			response: generated.RunResponse{Action: "run", Ok: true, Result: metadata, SchemaVersion: 1},
			warning:  err,
		}, nil
	}
	return dispatchResult{}, failureFromError(generated.ActionRun, err)
}

func (application *Application) dispatchWait(
	ctx context.Context,
	runtime runnerDispatch,
	request waitRequest,
) (dispatchResult, *failure) {
	prepared, err := runtime.manager.PrepareWait(ctx, runner.WaitRequest{
		Invocation: runtime.invocation, JobID: request.Payload.JobID, CursorBytes: request.Payload.CursorBytes,
		Timeout: durationFrom(request.Payload.TimeoutMs), IdleTimeout: durationFrom(request.Payload.IdleTimeoutMs),
	})
	if err != nil {
		return dispatchResult{}, failureFromError(request.action(), err)
	}
	return dispatchResult{
		response:   generated.WaitResponse{Action: "wait", Ok: true, Result: prepared.Observation, SchemaVersion: 1},
		afterWrite: prepared.Commit,
	}, nil
}

func operationResult(action generated.Action, response any, err error) (dispatchResult, *failure) {
	if err != nil {
		return dispatchResult{}, failureFromError(action, err)
	}
	return dispatchResult{response: response}, nil
}

func durationFrom(value *generated.TimeoutMs) time.Duration {
	if value == nil {
		return 0
	}
	return time.Duration(*value) * time.Millisecond
}

func intFrom(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

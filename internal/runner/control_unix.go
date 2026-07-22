//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

func (manager *Manager) Status(ctx context.Context, request StatusRequest) (generated.JobObservation, error) {
	if err := ctx.Err(); err != nil {
		return generated.JobObservation{}, err
	}
	store, err := manager.openStore(request.Invocation)
	if err != nil {
		return generated.JobObservation{}, err
	}
	result, operationErr := store.status(ctx, request.JobID)
	return result, errors.Join(operationErr, store.Close())
}

func (manager *Manager) Output(ctx context.Context, request OutputRequest) (generated.OutputObservation, error) {
	if err := ctx.Err(); err != nil {
		return generated.OutputObservation{}, err
	}
	store, err := manager.openStore(request.Invocation)
	if err != nil {
		return generated.OutputObservation{}, err
	}
	result, operationErr := store.output(ctx, request)
	return result, errors.Join(operationErr, store.Close())
}

func (manager *Manager) Cancel(ctx context.Context, request CancelRequest) (generated.CancellationResult, error) {
	if err := ctx.Err(); err != nil {
		return generated.CancellationResult{}, err
	}
	store, err := manager.openStore(request.Invocation)
	if err != nil {
		return generated.CancellationResult{}, err
	}
	result, operationErr := store.cancel(ctx, request.JobID)
	return result, errors.Join(operationErr, store.Close())
}

func (manager *Manager) List(ctx context.Context, request ListRequest) (generated.ListResult, error) {
	if err := ctx.Err(); err != nil {
		return generated.ListResult{}, err
	}
	store, err := manager.openStore(request.Invocation)
	if err != nil {
		return generated.ListResult{}, err
	}
	result, operationErr := store.list(ctx)
	return result, errors.Join(operationErr, store.Close())
}

func (manager *Manager) Remove(ctx context.Context, request RemoveRequest) (generated.RemoveResult, error) {
	if err := ctx.Err(); err != nil {
		return generated.RemoveResult{}, err
	}
	store, err := manager.openStore(request.Invocation)
	if err != nil {
		return generated.RemoveResult{}, err
	}
	removeErr := store.removeTerminal(ctx, request.JobID)
	closeErr := store.Close()
	if err := errors.Join(removeErr, closeErr); err != nil {
		return generated.RemoveResult{}, err
	}
	return generated.RemoveResult{JobID: request.JobID, Removed: true}, nil
}

func (manager *Manager) openStore(invocation state.TrustedInvocation) (*Store, error) {
	store, err := OpenStore(invocation, manager.contracts)
	if err != nil {
		return nil, err
	}
	store.lockTimeout = manager.config.StateLockTimeout
	store.lockPoll = manager.config.StateLockPoll
	store.beforeOutputRead = manager.beforeOutputRead
	store.afterListEntries = manager.afterListEntries
	return store, nil
}

func decisionError(code state.Code) error {
	switch code {
	case state.CodeJobNotFound:
		return ErrJobNotFound
	case state.CodeUnauthorized:
		return ErrUnauthorized
	case state.CodeActiveJob:
		return ErrActiveJob
	case state.CodeInvalidCursor:
		return ErrInvalidCursor
	case state.CodeInvalidRange:
		return ErrInvalidRange
	case state.CodeCorruptState, state.CodeInvalidStatus, state.CodeTransitionNotAllowed:
		return ErrCorruptState
	default:
		return fmt.Errorf("%w: %s", ErrInvalidStateUpdate, code)
	}
}

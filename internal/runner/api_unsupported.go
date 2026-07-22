//go:build !linux && !darwin

package runner

import (
	"context"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

func New(Config) (*Manager, error) {
	return nil, ErrUnsupported
}

func (*Manager) Start(context.Context, StartRequest) (generated.JobMetadata, error) {
	return generated.JobMetadata{}, ErrUnsupported
}

func (*Manager) Status(context.Context, StatusRequest) (generated.JobObservation, error) {
	return generated.JobObservation{}, ErrUnsupported
}

func (*Manager) Output(context.Context, OutputRequest) (generated.OutputObservation, error) {
	return generated.OutputObservation{}, ErrUnsupported
}

func (*Manager) PrepareWait(context.Context, WaitRequest) (*PreparedWait, error) {
	return nil, ErrUnsupported
}

func (*PreparedWait) commit(context.Context) error {
	return ErrUnsupported
}

func (*Manager) Cancel(context.Context, CancelRequest) (generated.CancellationResult, error) {
	return generated.CancellationResult{}, ErrUnsupported
}

func (*Manager) List(context.Context, ListRequest) (generated.ListResult, error) {
	return generated.ListResult{}, ErrUnsupported
}

func (*Manager) Remove(context.Context, RemoveRequest) (generated.RemoveResult, error) {
	return generated.RemoveResult{}, ErrUnsupported
}

//go:build !linux && !darwin

package runner

import (
	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

func OpenStore(_ state.TrustedInvocation, _ contract.Contracts) (*Store, error) {
	return nil, ErrUnsupported
}

func (*Store) Close() error {
	return ErrUnsupported
}

func (*Store) Load(generated.JobID) (Snapshot, error) {
	return Snapshot{}, ErrUnsupported
}

func (*Store) ReadOutput(generated.JobID) ([]byte, error) {
	return nil, ErrUnsupported
}

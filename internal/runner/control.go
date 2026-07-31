package runner

import (
	"context"
	"sync"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

type StatusRequest struct {
	Invocation state.TrustedInvocation
	JobID      generated.JobID
}

type OutputRequest struct {
	Invocation       state.TrustedInvocation
	JobID            generated.JobID
	StartCursorBytes *generated.ByteCursor
	EndCursorBytes   *generated.ByteCursor
}

type WaitRequest struct {
	Invocation  state.TrustedInvocation
	JobID       generated.JobID
	CursorBytes *generated.ByteCursor
	Timeout     time.Duration
	IdleTimeout time.Duration
}

type CancelRequest struct {
	Invocation state.TrustedInvocation
	JobID      generated.JobID
}

type ListRequest struct {
	Invocation state.TrustedInvocation
}

type RemoveRequest struct {
	Invocation state.TrustedInvocation
	JobID      generated.JobID
}

type PreparedWait struct {
	Observation generated.OutputObservation
	Reason      generated.ObservationReason

	mu         sync.Mutex
	manager    *Manager
	invocation state.TrustedInvocation
	jobID      generated.JobID
	cursor     generated.ByteCursor
	updatedAt  generated.TimestampUnixMs
	output     generated.OutputChunk
	committed  bool
}

func (prepared *PreparedWait) Commit(ctx context.Context) error {
	return prepared.commit(ctx)
}

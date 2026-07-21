package state

import (
	"slices"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

func (p Policy) CaptureLimit() ByteCount {
	return p.captureLimit
}

func (p Policy) Statuses() []generated.JobStatus {
	return slices.Clone(p.statuses)
}

func (p Policy) TTLEnabled() bool {
	return p.lifecycle.ttlEnabled
}

func (p Policy) RestartReattachmentEnabled() bool {
	return p.lifecycle.restartReattachment
}

func (p Policy) AuthorizeTransition(from generated.JobStatus, to generated.JobStatus) Decision {
	if !p.isKnownStatus(from) || !p.isKnownStatus(to) {
		return Decision{Allowed: false, Code: CodeInvalidStatus}
	}
	if _, allowed := p.transitions[transition{from: from, to: to}]; allowed {
		return Decision{Allowed: true, Code: CodeAllow}
	}
	return Decision{Allowed: false, Code: CodeTransitionNotAllowed}
}

func (p Policy) isKnownStatus(status generated.JobStatus) bool {
	return slices.Contains(p.statuses, status)
}

func (p Policy) isTerminal(status generated.JobStatus) bool {
	_, terminal := p.terminals[status]
	return terminal
}

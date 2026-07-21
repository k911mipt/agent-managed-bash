package state

import "github.com/k911mipt/agent-managed-bash/internal/protocol/generated"

func (p Policy) AuthorizeRead(access AccessContext) Decision {
	if access.JobWorkspace != access.RequestWorkspace {
		return Decision{Allowed: false, Code: p.access.crossWorkspaceRead}
	}
	return Decision{Allowed: true, Code: p.access.sameWorkspaceRead}
}

func (p Policy) AuthorizeMutation(access AccessContext) Decision {
	if access.JobWorkspace != access.RequestWorkspace {
		return Decision{Allowed: false, Code: p.access.crossWorkspaceMutation}
	}
	if access.OwnerSession != access.ActorSession {
		return Decision{Allowed: false, Code: p.access.nonOwnerMutation}
	}
	return Decision{Allowed: true, Code: p.access.ownerMutation}
}

func (p Policy) AuthorizeRemoval(status generated.JobStatus) Decision {
	if !p.isKnownStatus(status) {
		return Decision{Allowed: false, Code: CodeInvalidStatus}
	}
	if status == p.removal.activeStatus {
		return Decision{Allowed: false, Code: p.removal.activeCode}
	}
	return Decision{Allowed: true, Code: p.removal.terminalCode}
}

func (p Policy) EvaluateCancellation(context CancellationContext) CancellationDecision {
	if !p.isKnownStatus(context.Status) {
		return CancellationDecision{Decision: Decision{Allowed: false, Code: CodeInvalidStatus}}
	}
	if context.AlreadyRequested {
		return CancellationDecision{
			Decision: Decision{Allowed: true, Code: p.cancellation.repeatedCode},
		}
	}
	if p.isTerminal(context.Status) {
		return CancellationDecision{
			Decision: Decision{Allowed: true, Code: p.cancellation.terminalCode},
		}
	}
	return CancellationDecision{
		Decision:       Decision{Allowed: true, Code: p.cancellation.initialCode},
		PersistRequest: true,
	}
}

package state

import "github.com/k911mipt/agent-managed-bash/internal/protocol/generated"

func (invocation TrustedInvocation) SessionID() generated.SessionID {
	return invocation.sessionID
}

func (invocation TrustedInvocation) WorkspacePath() string {
	return invocation.workspacePath
}

func (invocation TrustedInvocation) Cwd() string {
	return invocation.cwd
}

func (p Policy) BindTrustedInvocation(
	host HostInvocation,
	asserted generated.TrustedContext,
) (TrustedInvocation, Decision) {
	if host.SessionID == "" {
		return TrustedInvocation{}, Decision{Allowed: false, Code: CodeUnauthorized}
	}
	pathDecision := p.validateWorkspaceDirectories(host.WorkspacePath, host.Cwd)
	if !pathDecision.Allowed {
		return TrustedInvocation{}, pathDecision
	}
	if asserted.SessionID != host.SessionID || asserted.WorkspacePath != host.WorkspacePath || asserted.Cwd != host.Cwd {
		return TrustedInvocation{}, Decision{Allowed: false, Code: CodeUnauthorized}
	}
	return TrustedInvocation{
		sessionID: host.SessionID, workspacePath: host.WorkspacePath, cwd: host.Cwd,
	}, Decision{Allowed: true, Code: CodeAllow}
}

func (p Policy) BindCapabilityInvocation(
	host HostInvocation,
	asserted generated.TrustedContext,
) (TrustedInvocation, Decision) {
	if host.SessionID == "" {
		return TrustedInvocation{}, Decision{Allowed: false, Code: CodeUnauthorized}
	}
	if _, decision := p.validateWorkspacePath(host.WorkspacePath, host.Cwd); !decision.Allowed {
		return TrustedInvocation{}, decision
	}
	if asserted.SessionID != host.SessionID || asserted.WorkspacePath != host.WorkspacePath || asserted.Cwd != host.Cwd {
		return TrustedInvocation{}, Decision{Allowed: false, Code: CodeUnauthorized}
	}
	return TrustedInvocation{
		sessionID: host.SessionID, workspacePath: host.WorkspacePath, cwd: host.Cwd,
	}, Decision{Allowed: true, Code: CodeAllow}
}

func (p Policy) AuthorizeTrustedRead(job generated.JobMetadata, invocation TrustedInvocation) Decision {
	return p.AuthorizeRead(AccessContext{
		JobWorkspace: job.WorkspacePath, RequestWorkspace: invocation.workspacePath,
		OwnerSession: job.OwnerSessionID, ActorSession: invocation.sessionID,
	})
}

func (p Policy) AuthorizeTrustedMutation(job generated.JobMetadata, invocation TrustedInvocation) Decision {
	return p.AuthorizeMutation(AccessContext{
		JobWorkspace: job.WorkspacePath, RequestWorkspace: invocation.workspacePath,
		OwnerSession: job.OwnerSessionID, ActorSession: invocation.sessionID,
	})
}

func (p Policy) EvaluateTrustedCancellation(
	job generated.JobMetadata,
	alreadyRequested bool,
	invocation TrustedInvocation,
) CancellationDecision {
	authorization := p.AuthorizeTrustedMutation(job, invocation)
	if !authorization.Allowed {
		return CancellationDecision{Decision: authorization}
	}
	return p.EvaluateCancellation(CancellationContext{Status: job.Status, AlreadyRequested: alreadyRequested})
}

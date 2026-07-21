package state

import "github.com/k911mipt/agent-managed-bash/internal/protocol/generated"

func (p Policy) ResolveWaitCursor(context WaitCursorContext) generated.ByteCursor {
	if context.Explicit != nil {
		return *context.Explicit
	}
	if context.Observer != nil {
		return context.Observer.CursorBytes
	}
	return p.observer.defaultCursor
}

func (p Policy) ObserverAfter(context ObserverAdvanceContext) (generated.ObserverCursor, Decision) {
	if context.Action != generated.ActionWait {
		return context.Current, Decision{Allowed: true, Code: CodeAllow}
	}
	decision := p.ValidateCursor(CursorContext{
		Cursor:   int64(context.Output.NextCursorBytes),
		Captured: int64(context.Output.CapturedBytes),
	})
	if !decision.Allowed {
		return generated.ObserverCursor{}, decision
	}
	return generated.ObserverCursor{
		SessionID:       context.Current.SessionID,
		CursorBytes:     context.Output.NextCursorBytes,
		UpdatedAtUnixMs: context.UpdatedAtUnixMs,
	}, decision
}

func (p Policy) WaitTimeout() generated.TimeoutMs {
	return p.defaults.waitTimeout
}

func (p Policy) IdleCheckpoint() generated.TimeoutMs {
	return p.defaults.idleCheckpoint
}

func (p Policy) HardTimeout() generated.TimeoutMs {
	return p.defaults.hardTimeout
}

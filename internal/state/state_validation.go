package state

import "github.com/k911mipt/agent-managed-bash/internal/protocol/generated"

func (p Policy) ValidatePersistedState(state generated.PersistedJobState) Decision {
	job := state.Job
	if state.SchemaVersion != 1 || state.Session.SchemaVersion != 1 {
		return corruptStateDecision()
	}
	if !p.isKnownStatus(job.Status) || job.OutputLimitBytes <= 0 || ByteCount(job.OutputLimitBytes) > p.captureLimit {
		return corruptStateDecision()
	}
	if state.Session.CreatedAtUnixMs > job.CreatedAtUnixMs || job.CreatedAtUnixMs > job.StartedAtUnixMs {
		return corruptStateDecision()
	}
	if job.CapturedBytes < 0 || int(job.CapturedBytes) > job.OutputLimitBytes {
		return corruptStateDecision()
	}
	if state.Session.SessionID != job.OwnerSessionID || state.Session.WorkspacePath != job.WorkspacePath {
		return corruptStateDecision()
	}
	if _, pathDecision := p.validateWorkspacePath(job.WorkspacePath, job.Cwd); !pathDecision.Allowed {
		return corruptStateDecision()
	}
	observerSessions := make(map[generated.SessionID]struct{}, len(state.Observers))
	for _, observer := range state.Observers {
		_, duplicate := observerSessions[observer.SessionID]
		if duplicate || observer.CursorBytes < 0 || observer.CursorBytes > job.CapturedBytes ||
			observer.UpdatedAtUnixMs < job.CreatedAtUnixMs {
			return corruptStateDecision()
		}
		observerSessions[observer.SessionID] = struct{}{}
	}
	if state.Cancellation != nil && state.Cancellation.RequestedAtUnixMs < job.CreatedAtUnixMs {
		return corruptStateDecision()
	}
	if state.Cancellation != nil && (!state.Cancellation.Requested || state.Cancellation.RequestedBySessionID != job.OwnerSessionID) {
		return corruptStateDecision()
	}
	if job.Status == generated.JobStatusRunning {
		if job.FinishedAtUnixMs != nil || state.Result != nil {
			return corruptStateDecision()
		}
		return Decision{Allowed: true, Code: CodeAllow}
	}
	if job.FinishedAtUnixMs == nil || state.Result == nil {
		return corruptStateDecision()
	}
	if *job.FinishedAtUnixMs < job.StartedAtUnixMs ||
		state.Cancellation != nil && state.Cancellation.RequestedAtUnixMs > *job.FinishedAtUnixMs {
		return corruptStateDecision()
	}
	for _, observer := range state.Observers {
		if observer.UpdatedAtUnixMs > *job.FinishedAtUnixMs {
			return corruptStateDecision()
		}
	}
	result := state.Result
	if generated.JobStatus(result.Status) != job.Status || result.CapturedBytes != job.CapturedBytes ||
		result.FinishedAtUnixMs != *job.FinishedAtUnixMs || !validProcessResult(*result) {
		return corruptStateDecision()
	}
	return Decision{Allowed: true, Code: CodeAllow}
}

func validProcessResult(result generated.ProcessResult) bool {
	switch result.Status {
	case generated.TerminalStatusSucceeded:
		return result.ExitCode != nil && *result.ExitCode == 0 && result.Signal == nil
	case generated.TerminalStatusNonzeroExit:
		return result.ExitCode != nil && *result.ExitCode > 0 && *result.ExitCode <= 255 && result.Signal == nil
	case generated.TerminalStatusSignalExit:
		return result.ExitCode == nil && result.Signal != nil && *result.Signal > 0 && *result.Signal <= 64
	case generated.TerminalStatusCancelled,
		generated.TerminalStatusHardTimeout,
		generated.TerminalStatusOutputLimit:
		return result.ExitCode == nil && (result.Signal == nil || *result.Signal > 0 && *result.Signal <= 64)
	case generated.TerminalStatusRunnerLost:
		return result.ExitCode == nil && result.Signal == nil && result.Diagnostic != nil && *result.Diagnostic != ""
	default:
		return false
	}
}

func corruptStateDecision() Decision {
	return Decision{Allowed: false, Code: CodeCorruptState}
}

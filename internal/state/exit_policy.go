package state

import "github.com/k911mipt/agent-managed-bash/internal/protocol/generated"

func (p Policy) SuccessExitClass() generated.ExitClass {
	return p.exit.success
}

func (p Policy) ExitClassForError(code generated.ErrorCode) (generated.ExitClass, bool) {
	exitClass, found := p.exit.byError[code]
	return exitClass, found
}

func (p Policy) ProtocolOutcomeForCode(code Code) (ProtocolOutcome, bool) {
	errorCode, found := p.exit.byCode[code]
	if !found {
		return ProtocolOutcome{}, false
	}
	if errorCode == nil {
		return ProtocolOutcome{Kind: ProtocolOutcomeSuccess, ExitClass: p.exit.success}, true
	}
	exitClass, found := p.exit.byError[*errorCode]
	if !found {
		return ProtocolOutcome{}, false
	}
	return ProtocolOutcome{Kind: ProtocolOutcomeError, ErrorCode: *errorCode, ExitClass: exitClass}, true
}

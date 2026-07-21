package state

import "github.com/k911mipt/agent-managed-bash/internal/protocol/generated"

type Code string

const (
	CodeAllow                  Code = "allow"
	CodeTransitionNotAllowed   Code = "transition_not_allowed"
	CodeInvalidStatus          Code = "invalid_status"
	CodeJobNotFound            Code = "job_not_found"
	CodeUnauthorized           Code = "unauthorized"
	CodeActiveJob              Code = "active_job"
	CodeCancellationRequested  Code = "cancellation_requested"
	CodeCancellationIdempotent Code = "cancellation_idempotent"
	CodeCancellationTerminal   Code = "cancellation_terminal"
	CodeInvalidCursor          Code = "invalid_cursor"
	CodeInvalidRange           Code = "invalid_range"
	CodePathInvalid            Code = "path_invalid"
	CodePathOutsideWorkspace   Code = "path_outside_workspace"
	CodePathSymlink            Code = "path_symlink"
	CodePathUnavailable        Code = "path_unavailable"
	CodeCorruptState           Code = "corrupt_state"
)

const (
	ExitClassSuccess       generated.ExitClass = 0
	ExitClassValidation    generated.ExitClass = 2
	ExitClassAuthorization generated.ExitClass = 3
	ExitClassConflict      generated.ExitClass = 4
	ExitClassInternal      generated.ExitClass = 5
)

type Decision struct {
	Allowed bool
	Code    Code
}

type AccessContext struct {
	JobWorkspace     string
	RequestWorkspace string
	OwnerSession     generated.SessionID
	ActorSession     generated.SessionID
}

type CancellationContext struct {
	Status           generated.JobStatus
	AlreadyRequested bool
}

type CancellationDecision struct {
	Decision
	PersistRequest bool
}

type CursorContext struct {
	Cursor   int64
	Captured int64
}

type RangeContext struct {
	Start    int64
	End      int64
	Captured int64
}

type OutputContext struct {
	Range    RangeContext
	Terminal bool
}

type WaitCursorContext struct {
	Explicit *generated.ByteCursor
	Observer *generated.ObserverCursor
}

type ObserverAdvanceContext struct {
	Action          generated.Action
	Current         generated.ObserverCursor
	Output          generated.OutputChunk
	UpdatedAtUnixMs generated.TimestampUnixMs
}

type HostInvocation struct {
	SessionID     generated.SessionID
	WorkspacePath string
	Cwd           string
}

type TrustedInvocation struct {
	sessionID     generated.SessionID
	workspacePath string
	cwd           string
}

type ProtocolOutcome struct {
	Kind      ProtocolOutcomeKind
	ErrorCode generated.ErrorCode
	ExitClass generated.ExitClass
}

type ProtocolOutcomeKind uint8

const (
	ProtocolOutcomeSuccess ProtocolOutcomeKind = iota
	ProtocolOutcomeError
)

type ByteCount uint64

type transition struct {
	from generated.JobStatus
	to   generated.JobStatus
}

type accessRules struct {
	sameWorkspaceRead      Code
	crossWorkspaceRead     Code
	ownerMutation          Code
	nonOwnerMutation       Code
	crossWorkspaceMutation Code
}

type removalRules struct {
	activeStatus generated.JobStatus
	activeCode   Code
	terminalCode Code
}

type cancellationRules struct {
	initialCode  Code
	repeatedCode Code
	terminalCode Code
}

type cursorRules struct {
	invalidCursorCode Code
	invalidRangeCode  Code
}

type pathRules struct {
	invalidCode     Code
	outsideCode     Code
	symlinkCode     Code
	unavailableCode Code
}

type defaultsRules struct {
	waitTimeout    generated.TimeoutMs
	idleCheckpoint generated.TimeoutMs
	hardTimeout    generated.TimeoutMs
}

type observerRules struct {
	defaultCursor generated.ByteCursor
}

type lifecycleRules struct {
	ttlEnabled          bool
	restartReattachment bool
}

type exitRules struct {
	success generated.ExitClass
	byError map[generated.ErrorCode]generated.ExitClass
	byCode  map[Code]*generated.ErrorCode
}

type Policy struct {
	captureLimit ByteCount
	exit         exitRules
	statuses     []generated.JobStatus
	terminals    map[generated.JobStatus]struct{}
	transitions  map[transition]struct{}
	access       accessRules
	removal      removalRules
	cancellation cancellationRules
	cursor       cursorRules
	path         pathRules
	defaults     defaultsRules
	observer     observerRules
	lifecycle    lifecycleRules
}

package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const policySchemaURL = "https://agent-managed-bash.dev/schema/v1/policy.schema.json"

var (
	ErrPolicySchema     = errors.New("policy schema is invalid")
	ErrPolicyValidation = errors.New("policy document is invalid")
)

type policyDocument struct {
	SchemaVersion     int                   `json:"schema_version"`
	CaptureLimitBytes uint64                `json:"capture_limit_bytes"`
	Defaults          defaultsDocument      `json:"defaults"`
	Observer          observerDocument      `json:"observer"`
	ExitClasses       exitClassesDocument   `json:"exit_classes"`
	ErrorExitClasses  errorClassesDocument  `json:"error_exit_classes"`
	CodeErrorMapping  codeMappingDocument   `json:"code_error_mapping"`
	Statuses          []generated.JobStatus `json:"statuses"`
	TerminalStatuses  []generated.JobStatus `json:"terminal_statuses"`
	Transitions       []transitionDocument  `json:"transitions"`
	Access            accessDocument        `json:"access"`
	Removal           removalDocument       `json:"removal"`
	Cancellation      cancellationDocument  `json:"cancellation"`
	Cursor            cursorDocument        `json:"cursor"`
	Path              pathDocument          `json:"path"`
	Lifecycle         lifecycleDocument     `json:"lifecycle"`
}

type defaultsDocument struct {
	WaitTimeoutMs     generated.TimeoutMs `json:"wait_timeout_ms"`
	IdleCheckpointMs  generated.TimeoutMs `json:"idle_checkpoint_ms"`
	HardTimeoutMs     generated.TimeoutMs `json:"hard_timeout_ms"`
	CaptureLimitBytes uint64              `json:"capture_limit_bytes"`
}

type observerDocument struct {
	DefaultCursorBytes generated.ByteCursor `json:"default_cursor_bytes"`
	WaitOmittedCursor  string               `json:"wait_omitted_cursor"`
	WaitSuccess        string               `json:"wait_success"`
	OtherActions       string               `json:"other_actions"`
}

type codeMappingDocument struct {
	Allow                  *generated.ErrorCode `json:"allow"`
	CancellationRequested  *generated.ErrorCode `json:"cancellation_requested"`
	CancellationIdempotent *generated.ErrorCode `json:"cancellation_idempotent"`
	CancellationTerminal   *generated.ErrorCode `json:"cancellation_terminal"`
	TransitionNotAllowed   *generated.ErrorCode `json:"transition_not_allowed"`
	InvalidStatus          *generated.ErrorCode `json:"invalid_status"`
	CorruptState           *generated.ErrorCode `json:"corrupt_state"`
	JobNotFound            *generated.ErrorCode `json:"job_not_found"`
	Unauthorized           *generated.ErrorCode `json:"unauthorized"`
	ActiveJob              *generated.ErrorCode `json:"active_job"`
	InvalidCursor          *generated.ErrorCode `json:"invalid_cursor"`
	InvalidRange           *generated.ErrorCode `json:"invalid_range"`
	PathInvalid            *generated.ErrorCode `json:"path_invalid"`
	PathOutsideWorkspace   *generated.ErrorCode `json:"path_outside_workspace"`
	PathSymlink            *generated.ErrorCode `json:"path_symlink"`
	PathUnavailable        *generated.ErrorCode `json:"path_unavailable"`
}

type exitClassesDocument struct {
	Success       generated.ExitClass `json:"success"`
	Validation    generated.ExitClass `json:"validation"`
	Authorization generated.ExitClass `json:"authorization"`
	Conflict      generated.ExitClass `json:"conflict"`
	Internal      generated.ExitClass `json:"internal"`
}

type errorClassesDocument struct {
	MalformedJSON       generated.ExitClass `json:"malformed_json"`
	InvalidRequest      generated.ExitClass `json:"invalid_request"`
	IncompatibleVersion generated.ExitClass `json:"incompatible_version"`
	InvalidRange        generated.ExitClass `json:"invalid_range"`
	InvalidCursor       generated.ExitClass `json:"invalid_cursor"`
	JobNotFound         generated.ExitClass `json:"job_not_found"`
	Unauthorized        generated.ExitClass `json:"unauthorized"`
	ActiveJob           generated.ExitClass `json:"active_job"`
	Conflict            generated.ExitClass `json:"conflict"`
	CorruptState        generated.ExitClass `json:"corrupt_state"`
	RunnerUnavailable   generated.ExitClass `json:"runner_unavailable"`
	IOFailure           generated.ExitClass `json:"io_failure"`
	Internal            generated.ExitClass `json:"internal"`
}

type transitionDocument struct {
	From generated.JobStatus `json:"from"`
	To   generated.JobStatus `json:"to"`
}

type accessDocument struct {
	SameWorkspaceRead      Code `json:"same_workspace_read"`
	CrossWorkspaceRead     Code `json:"cross_workspace_read"`
	OwnerMutation          Code `json:"owner_mutation"`
	NonOwnerMutation       Code `json:"non_owner_mutation"`
	CrossWorkspaceMutation Code `json:"cross_workspace_mutation"`
}

type removalDocument struct {
	ActiveStatus generated.JobStatus `json:"active_status"`
	ActiveCode   Code                `json:"active_code"`
	TerminalCode Code                `json:"terminal_code"`
}

type cancellationDocument struct {
	InitialCode  Code `json:"initial_code"`
	RepeatedCode Code `json:"repeated_code"`
	TerminalCode Code `json:"terminal_code"`
}

type cursorDocument struct {
	InvalidCursorCode Code `json:"invalid_cursor_code"`
	InvalidRangeCode  Code `json:"invalid_range_code"`
}

type pathDocument struct {
	InvalidCode     Code `json:"invalid_code"`
	OutsideCode     Code `json:"outside_code"`
	SymlinkCode     Code `json:"symlink_code"`
	UnavailableCode Code `json:"unavailable_code"`
}

type lifecycleDocument struct {
	TTLEnabled          bool `json:"ttl_enabled"`
	RestartReattachment bool `json:"restart_reattachment"`
}

func LoadPolicy(rawSchema []byte, rawPolicy []byte) (Policy, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	schemaDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawSchema))
	if err != nil {
		return Policy{}, fmt.Errorf("%w: decode: %w", ErrPolicySchema, err)
	}
	if err := compiler.AddResource(policySchemaURL, schemaDocument); err != nil {
		return Policy{}, fmt.Errorf("%w: register: %w", ErrPolicySchema, err)
	}
	schema, err := compiler.Compile(policySchemaURL)
	if err != nil {
		return Policy{}, fmt.Errorf("%w: compile: %w", ErrPolicySchema, err)
	}
	policyValue, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawPolicy))
	if err != nil {
		return Policy{}, fmt.Errorf("%w: decode: %w", ErrPolicyValidation, err)
	}
	if err := schema.Validate(policyValue); err != nil {
		return Policy{}, fmt.Errorf("%w: validate: %w", ErrPolicyValidation, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(rawPolicy))
	decoder.DisallowUnknownFields()
	var document policyDocument
	if err := decoder.Decode(&document); err != nil {
		return Policy{}, fmt.Errorf("%w: parse typed document: %w", ErrPolicyValidation, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Policy{}, fmt.Errorf("%w: trailing data", ErrPolicyValidation)
	}
	return newPolicy(document), nil
}

func newPolicy(document policyDocument) Policy {
	terminals := make(map[generated.JobStatus]struct{}, len(document.TerminalStatuses))
	for _, status := range document.TerminalStatuses {
		terminals[status] = struct{}{}
	}
	transitions := make(map[transition]struct{}, len(document.Transitions))
	for _, item := range document.Transitions {
		transitions[transition{from: item.From, to: item.To}] = struct{}{}
	}
	return Policy{
		captureLimit: ByteCount(document.CaptureLimitBytes),
		exit: exitRules{
			success: document.ExitClasses.Success,
			byError: map[generated.ErrorCode]generated.ExitClass{
				generated.ErrorCodeMalformedJson:       document.ErrorExitClasses.MalformedJSON,
				generated.ErrorCodeInvalidRequest:      document.ErrorExitClasses.InvalidRequest,
				generated.ErrorCodeIncompatibleVersion: document.ErrorExitClasses.IncompatibleVersion,
				generated.ErrorCodeInvalidRange:        document.ErrorExitClasses.InvalidRange,
				generated.ErrorCodeInvalidCursor:       document.ErrorExitClasses.InvalidCursor,
				generated.ErrorCodeJobNotFound:         document.ErrorExitClasses.JobNotFound,
				generated.ErrorCodeUnauthorized:        document.ErrorExitClasses.Unauthorized,
				generated.ErrorCodeActiveJob:           document.ErrorExitClasses.ActiveJob,
				generated.ErrorCodeConflict:            document.ErrorExitClasses.Conflict,
				generated.ErrorCodeCorruptState:        document.ErrorExitClasses.CorruptState,
				generated.ErrorCodeRunnerUnavailable:   document.ErrorExitClasses.RunnerUnavailable,
				generated.ErrorCodeIoFailure:           document.ErrorExitClasses.IOFailure,
				generated.ErrorCodeInternal:            document.ErrorExitClasses.Internal,
			},
			byCode: map[Code]*generated.ErrorCode{
				CodeAllow:                  document.CodeErrorMapping.Allow,
				CodeCancellationRequested:  document.CodeErrorMapping.CancellationRequested,
				CodeCancellationIdempotent: document.CodeErrorMapping.CancellationIdempotent,
				CodeCancellationTerminal:   document.CodeErrorMapping.CancellationTerminal,
				CodeTransitionNotAllowed:   document.CodeErrorMapping.TransitionNotAllowed,
				CodeInvalidStatus:          document.CodeErrorMapping.InvalidStatus,
				CodeCorruptState:           document.CodeErrorMapping.CorruptState,
				CodeJobNotFound:            document.CodeErrorMapping.JobNotFound,
				CodeUnauthorized:           document.CodeErrorMapping.Unauthorized,
				CodeActiveJob:              document.CodeErrorMapping.ActiveJob,
				CodeInvalidCursor:          document.CodeErrorMapping.InvalidCursor,
				CodeInvalidRange:           document.CodeErrorMapping.InvalidRange,
				CodePathInvalid:            document.CodeErrorMapping.PathInvalid,
				CodePathOutsideWorkspace:   document.CodeErrorMapping.PathOutsideWorkspace,
				CodePathSymlink:            document.CodeErrorMapping.PathSymlink,
				CodePathUnavailable:        document.CodeErrorMapping.PathUnavailable,
			},
		},
		statuses:    document.Statuses,
		terminals:   terminals,
		transitions: transitions,
		access: accessRules{
			sameWorkspaceRead: document.Access.SameWorkspaceRead, crossWorkspaceRead: document.Access.CrossWorkspaceRead,
			ownerMutation: document.Access.OwnerMutation, nonOwnerMutation: document.Access.NonOwnerMutation,
			crossWorkspaceMutation: document.Access.CrossWorkspaceMutation,
		},
		removal: removalRules{document.Removal.ActiveStatus, document.Removal.ActiveCode, document.Removal.TerminalCode},
		cancellation: cancellationRules{
			document.Cancellation.InitialCode, document.Cancellation.RepeatedCode, document.Cancellation.TerminalCode,
		},
		cursor: cursorRules{document.Cursor.InvalidCursorCode, document.Cursor.InvalidRangeCode},
		path: pathRules{
			document.Path.InvalidCode, document.Path.OutsideCode, document.Path.SymlinkCode,
			document.Path.UnavailableCode,
		},
		defaults: defaultsRules{
			waitTimeout:    document.Defaults.WaitTimeoutMs,
			idleCheckpoint: document.Defaults.IdleCheckpointMs,
			hardTimeout:    document.Defaults.HardTimeoutMs,
		},
		observer:  observerRules{defaultCursor: document.Observer.DefaultCursorBytes},
		lifecycle: lifecycleRules{document.Lifecycle.TTLEnabled, document.Lifecycle.RestartReattachment},
	}
}

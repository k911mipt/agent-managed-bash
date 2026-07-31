package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"unicode"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
)

type failure struct {
	action           *generated.Action
	diagnosticAction generated.Action
	code             generated.ErrorCode
	details          *generated.ErrorDetails
	job              *generated.JobMetadata
	cause            error
}

func newProblem(code generated.ErrorCode, cause error) *failure {
	return &failure{code: code, cause: cause}
}

func newDetailedProblem(code generated.ErrorCode, details *generated.ErrorDetails, cause error) *failure {
	return &failure{code: code, details: details, cause: cause}
}

func newFailure(action generated.Action, problem *failure) *failure {
	problem.action = actionPointer(action)
	problem.diagnosticAction = action
	return problem
}

func newFailureWithoutAction(diagnosticAction generated.Action, problem *failure) *failure {
	problem.diagnosticAction = diagnosticAction
	return problem
}

func (application *Application) writeFailure(streams Streams, problem *failure) int {
	response := generated.ErrorResponse{
		Action:        problem.action,
		Error:         generated.ProtocolError{Code: problem.code, Message: publicErrorMessage(problem.code), Details: problem.details},
		Job:           problem.job,
		Ok:            false,
		SchemaVersion: 1,
	}
	if err := application.writeResponse(streams.Stdout, response); err != nil {
		return application.writeOutputFailure(streams.Stderr, problem.diagnosticAction, err)
	}
	_, _ = fmt.Fprintf(
		streams.Stderr,
		"managed-bash: action=%s code=%s: %s\n",
		problem.diagnosticAction,
		problem.code,
		publicErrorMessage(problem.code),
	)
	exitClass, found := application.contracts.Policy().ExitClassForError(problem.code)
	if !found {
		return int(generated.ExitClass(5))
	}
	return int(exitClass)
}

func (*Application) writeOutputFailure(stderr io.Writer, action generated.Action, cause error) int {
	code := generated.ErrorCodeIoFailure
	var invariantError *responseInvariantError
	if errors.As(cause, &invariantError) {
		code = generated.ErrorCodeInternal
	}
	_, _ = fmt.Fprintf(stderr, "managed-bash: action=%s code=%s: %s\n", action, code, Diagnostic(cause))
	return int(generated.ExitClass(5))
}

func Diagnostic(err error) string {
	if err == nil {
		return "unknown failure"
	}
	const maximumRunes = 512
	var builder strings.Builder
	previousSpace := false
	truncated := false
	runeCount := 0
	for _, value := range err.Error() {
		if unicode.IsControl(value) || unicode.IsSpace(value) {
			value = ' '
		}
		if value == ' ' {
			if previousSpace || builder.Len() == 0 {
				continue
			}
			previousSpace = true
		} else {
			previousSpace = false
		}
		if runeCount >= maximumRunes-1 {
			truncated = true
			break
		}
		builder.WriteRune(value)
		runeCount++
	}
	result := strings.TrimSpace(builder.String())
	if truncated {
		result = strings.TrimSpace(result) + "…"
	}
	if result == "" {
		return "unknown failure"
	}
	return result
}

func publicErrorMessage(code generated.ErrorCode) string {
	switch code {
	case generated.ErrorCodeMalformedJson:
		return "request is not JSON"
	case generated.ErrorCodeInvalidRequest:
		return "request is invalid"
	case generated.ErrorCodeIncompatibleVersion:
		return "protocol version is incompatible"
	case generated.ErrorCodeInvalidRange:
		return "output range is invalid"
	case generated.ErrorCodeInvalidCursor:
		return "cursor is invalid"
	case generated.ErrorCodeJobNotFound:
		return "job was not found"
	case generated.ErrorCodeUnauthorized:
		return "request is unauthorized"
	case generated.ErrorCodeActiveJob:
		return "job is active"
	case generated.ErrorCodeConflict:
		return "request conflicts with job state"
	case generated.ErrorCodeCorruptState:
		return "persisted state is corrupt"
	case generated.ErrorCodeRunnerUnavailable:
		return "runner is unavailable"
	case generated.ErrorCodeIoFailure:
		return "I/O operation failed"
	case generated.ErrorCodeInternal:
		return "internal failure"
	default:
		return "internal failure"
	}
}

func failureFromError(action generated.Action, err error) *failure {
	code := generated.ErrorCodeInternal
	switch {
	case errors.Is(err, runner.ErrInvalidStart), errors.Is(err, runner.ErrInvalidJobID):
		code = generated.ErrorCodeInvalidRequest
	case errors.Is(err, runner.ErrInvalidRange):
		code = generated.ErrorCodeInvalidRange
	case errors.Is(err, runner.ErrInvalidCursor):
		code = generated.ErrorCodeInvalidCursor
	case errors.Is(err, runner.ErrJobNotFound):
		code = generated.ErrorCodeJobNotFound
	case errors.Is(err, runner.ErrUnauthorized), errors.Is(err, runner.ErrUnsafeFilesystem):
		code = generated.ErrorCodeUnauthorized
	case errors.Is(err, runner.ErrActiveJob):
		code = generated.ErrorCodeActiveJob
	case errors.Is(err, runner.ErrStateLockTimeout), errors.Is(err, runner.ErrJobExists),
		errors.Is(err, runner.ErrRunnerActive), errors.Is(err, runner.ErrInvalidStateUpdate):
		code = generated.ErrorCodeConflict
	case errors.Is(err, runner.ErrCorruptState), errors.Is(err, runner.ErrInvalidRuntime):
		code = generated.ErrorCodeCorruptState
	case errors.Is(err, runner.ErrStartupTimeout), errors.Is(err, runner.ErrStartupFailed),
		errors.Is(err, runner.ErrInternalProtocol), errors.Is(err, runner.ErrExecution),
		errors.Is(err, runner.ErrUnsupported):
		code = generated.ErrorCodeRunnerUnavailable
	case isIOFailure(err):
		code = generated.ErrorCodeIoFailure
	}
	return newFailure(action, newProblem(code, err))
}

func isIOFailure(err error) bool {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrClosed) ||
		errors.Is(err, io.ErrShortWrite) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	var pathError *os.PathError
	var linkError *os.LinkError
	var syscallError *os.SyscallError
	var errno syscall.Errno
	return errors.As(err, &pathError) || errors.As(err, &linkError) || errors.As(err, &syscallError) || errors.As(err, &errno)
}

func actionPointer(action generated.Action) *generated.Action {
	return &action
}

func stringPointer(value string) *string {
	return &value
}

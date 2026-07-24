package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

const (
	hostSessionEnvironment   = "MANAGED_BASH_HOST_SESSION_ID"
	hostWorkspaceEnvironment = "MANAGED_BASH_HOST_WORKSPACE_PATH"
)

var errMissingHostContext = errors.New("trusted host context is missing")

func (application *Application) bindInvocation(
	action generated.Action,
	asserted generated.TrustedContext,
) (state.TrustedInvocation, *failure) {
	host, err := readHostInvocation()
	if err != nil {
		if errors.Is(err, errMissingHostContext) {
			return state.TrustedInvocation{}, newFailure(action, newProblem(generated.ErrorCodeUnauthorized, err))
		}
		return state.TrustedInvocation{}, newFailure(action, newProblem(generated.ErrorCodeIoFailure, err))
	}
	invocation, decision := application.contracts.Policy().BindTrustedInvocation(host, asserted)
	if decision.Allowed {
		return invocation, nil
	}
	outcome, found := application.contracts.Policy().ProtocolOutcomeForCode(decision.Code)
	if !found || outcome.Kind != state.ProtocolOutcomeError {
		return state.TrustedInvocation{}, newFailure(action, newProblem(generated.ErrorCodeInternal, fmt.Errorf("unmapped host decision %s", decision.Code)))
	}
	cause := fmt.Errorf("trusted host context rejected: %s", decision.Code)
	details := contextFailureDetails(host, asserted, decision)
	if details == nil {
		return state.TrustedInvocation{}, newFailure(action, newProblem(outcome.ErrorCode, cause))
	}
	return state.TrustedInvocation{}, newFailure(action, newDetailedProblem(outcome.ErrorCode, details, cause))
}

func contextFailureDetails(
	host state.HostInvocation,
	asserted generated.TrustedContext,
	decision state.Decision,
) *generated.ErrorDetails {
	switch decision.Code {
	case state.CodePathInvalid:
		if host.WorkspacePath == string(filepath.Separator) || !canonicalPath(host.WorkspacePath) {
			return fieldConstraintDetails(
				"context.workspace_path", "physical canonical workspace directory other than /", host.WorkspacePath,
			)
		}
		return fieldConstraintDetails("context.cwd", "physical canonical directory inside workspace", host.Cwd)
	case state.CodePathOutsideWorkspace:
		return fieldConstraintDetails("context.cwd", "physical canonical directory inside workspace", host.Cwd)
	case state.CodeUnauthorized:
		if asserted.SessionID != host.SessionID {
			return fieldConstraintDetails("context.session_id", "trusted host session", string(asserted.SessionID))
		}
		if asserted.WorkspacePath != host.WorkspacePath {
			return fieldConstraintDetails("context.workspace_path", "trusted host workspace", asserted.WorkspacePath)
		}
		if asserted.Cwd != host.Cwd {
			return fieldConstraintDetails("context.cwd", "trusted host working directory", asserted.Cwd)
		}
	}
	return nil
}

func canonicalPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}

func fieldConstraintDetails(field string, expected string, actual string) *generated.ErrorDetails {
	if actual != "" {
		actual = Diagnostic(errors.New(actual))
	}
	return &generated.ErrorDetails{
		Field: stringPointer(field), Expected: stringPointer(expected), Actual: stringPointer(actual),
	}
}

func readHostInvocation() (state.HostInvocation, error) {
	session := os.Getenv(hostSessionEnvironment)
	workspace := os.Getenv(hostWorkspaceEnvironment)
	scrubErr := errors.Join(
		os.Unsetenv(hostSessionEnvironment),
		os.Unsetenv(hostWorkspaceEnvironment),
		os.Unsetenv("PWD"),
	)
	if session == "" || workspace == "" {
		return state.HostInvocation{}, errors.Join(errMissingHostContext, scrubErr)
	}
	if scrubErr != nil {
		return state.HostInvocation{}, fmt.Errorf("scrub trusted host environment: %w", scrubErr)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return state.HostInvocation{}, fmt.Errorf("resolve physical working directory: %w", err)
	}
	return state.HostInvocation{
		SessionID: generated.SessionID(session), WorkspacePath: workspace, Cwd: cwd,
	}, nil
}

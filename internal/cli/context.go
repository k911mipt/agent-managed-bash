package cli

import (
	"errors"
	"fmt"
	"os"

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
	return state.TrustedInvocation{}, newFailure(action, newProblem(outcome.ErrorCode, fmt.Errorf("trusted host context rejected: %s", decision.Code)))
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

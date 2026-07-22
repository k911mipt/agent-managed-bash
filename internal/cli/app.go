package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

type Config struct {
	BinaryVersion string
	Runner        runner.Config
}

type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type Application struct {
	config    Config
	contracts contract.Contracts
	validator *protocol.Validator
}

const usage = "usage: managed-bash <run|wait|status|output|cancel|remove|list|version>"

func New(config Config) (*Application, error) {
	if config.BinaryVersion == "" {
		config.BinaryVersion = "dev"
	}
	contracts, err := contract.Load()
	if err != nil {
		return nil, fmt.Errorf("load contracts: %w", err)
	}
	validator, err := protocol.NewValidator()
	if err != nil {
		return nil, fmt.Errorf("load protocol validator: %w", err)
	}
	return &Application{config: config, contracts: contracts, validator: validator}, nil
}

func (application *Application) Execute(ctx context.Context, args []string, streams Streams) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, _ = fmt.Fprintln(streams.Stdout, usage)
		return int(application.contracts.Policy().SuccessExitClass())
	}
	action, ok := parseAction(args)
	if !ok {
		_, _ = fmt.Fprintln(streams.Stderr, usage)
		return int(generated.ExitClass(2))
	}
	request, requestFailure := application.readRequest(action, streams.Stdin)
	if requestFailure != nil {
		return application.writeFailure(streams, requestFailure)
	}

	var invocation state.TrustedInvocation
	if asserted, required := request.value.assertedContext(); required {
		var contextFailure *failure
		invocation, contextFailure = application.bindInvocation(request.value.action(), asserted)
		if contextFailure != nil {
			return application.writeFailure(streams, contextFailure)
		}
	}
	result, dispatchFailure := application.dispatch(ctx, request.value, invocation)
	if dispatchFailure != nil {
		return application.writeFailure(streams, dispatchFailure)
	}
	exitCode := application.writeSuccess(streams, request.value.action(), result.response)
	if exitCode != int(application.contracts.Policy().SuccessExitClass()) {
		return exitCode
	}
	if result.warning != nil {
		_, _ = fmt.Fprintf(streams.Stderr, "managed-bash: warning: action=%s: %s\n", request.value.action(), Diagnostic(result.warning))
	}
	if result.afterWrite != nil {
		if err := result.afterWrite(ctx); err != nil {
			_, _ = fmt.Fprintf(streams.Stderr, "managed-bash: warning: action=%s: response delivered but observer cursor was not committed: %s\n", request.value.action(), Diagnostic(err))
		}
	}
	return int(application.contracts.Policy().SuccessExitClass())
}

func parseAction(args []string) (generated.Action, bool) {
	if len(args) != 1 {
		return "", false
	}
	return knownAction(args[0])
}

func knownAction(raw string) (generated.Action, bool) {
	switch generated.Action(raw) {
	case generated.ActionRun, generated.ActionWait, generated.ActionStatus, generated.ActionOutput,
		generated.ActionCancel, generated.ActionRemove, generated.ActionList, generated.ActionVersion:
		return generated.Action(raw), true
	default:
		return "", false
	}
}

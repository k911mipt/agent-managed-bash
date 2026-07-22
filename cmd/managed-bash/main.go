package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/k911mipt/agent-managed-bash/internal/cli"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

var binaryVersion = "dev"

func main() {
	os.Exit(run(os.Args[1:], cli.Streams{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}

func run(args []string, streams cli.Streams) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	handled, err := runner.DispatchInternal(ctx, args)
	if handled {
		if err != nil {
			_, _ = fmt.Fprintf(streams.Stderr, "managed-bash: internal runner failed: %s\n", cli.Diagnostic(err))
			return int(state.ExitClassInternal)
		}
		return int(state.ExitClassSuccess)
	}
	application, err := cli.New(cli.Config{BinaryVersion: binaryVersion})
	if err != nil {
		_, _ = fmt.Fprintf(streams.Stderr, "managed-bash: initialize CLI: %s\n", cli.Diagnostic(err))
		return int(state.ExitClassInternal)
	}
	return application.Execute(ctx, args, streams)
}

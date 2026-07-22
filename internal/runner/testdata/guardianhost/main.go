package main

import (
	"context"
	"fmt"
	"os"

	"github.com/k911mipt/agent-managed-bash/internal/runner"
)

func main() {
	handled, err := runner.DispatchInternal(context.Background(), os.Args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !handled {
		os.Exit(2)
	}
}

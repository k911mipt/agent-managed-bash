//go:build linux || darwin

package runner_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
)

const (
	testStartupTimeout    = 3 * time.Second
	testTerminationGrace  = 40 * time.Millisecond
	testLifecycleDeadline = 5 * time.Second
)

type lifecycleResult struct {
	job       generated.JobMetadata
	snapshot  runner.Snapshot
	output    []byte
	workspace string
}

func TestMain(m *testing.M) {
	if os.Getenv(testCrashChildEnvironment) == "1" {
		_ = prepareInternalChildDiagnostics([]string{"--managed-bash-internal=runner"})
		panic("intentional lifecycle diagnostic crash")
	}
	diagnosticsDirectory := os.Getenv(testDiagnosticsEnvironment)
	ownsDiagnosticsDirectory := false
	if _, internal := internalChildRole(os.Args[1:]); !internal && diagnosticsDirectory == "" {
		var err error
		diagnosticsDirectory, err = os.MkdirTemp("", "managed-bash-lifecycle-diagnostics-")
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.Setenv(testDiagnosticsEnvironment, diagnosticsDirectory); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			_ = os.RemoveAll(diagnosticsDirectory)
			os.Exit(1)
		}
		ownsDiagnosticsDirectory = true
	}
	diagnostics := prepareInternalChildDiagnostics(os.Args[1:])
	handled, err := runner.DispatchInternal(context.Background(), os.Args[1:])
	diagnostics.recordDispatch(handled, err)
	if handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	exitCode := m.Run()
	if ownsDiagnosticsDirectory {
		if err := os.RemoveAll(diagnosticsDirectory); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			exitCode = 1
		}
	}
	os.Exit(exitCode)
}

func runLifecycle(
	t *testing.T,
	command string,
	hardTimeout time.Duration,
	outputLimit int,
) lifecycleResult {
	t.Helper()
	workspace := runner.NewTestWorkspace(t)
	invocation := trustedInvocation(t, workspace, workspace)
	executable, err := os.Executable()
	require.NoError(t, err)
	manager, err := runner.New(runner.Config{
		Executable: executable, StartupTimeout: testStartupTimeout, TerminationGrace: testTerminationGrace,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), testLifecycleDeadline)
	defer cancel()
	job, err := manager.Start(ctx, runner.StartRequest{
		Invocation: invocation, Command: command, HardTimeout: hardTimeout, OutputLimitBytes: outputLimit,
	})
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusRunning, job.Status)
	contracts, err := contract.Load()
	require.NoError(t, err)
	store, err := runner.OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	snapshot := waitForTerminal(t, store, job.JobID, testLifecycleDeadline)
	output, err := store.ReadOutput(job.JobID)
	require.NoError(t, err)
	return lifecycleResult{job: job, snapshot: snapshot, output: output, workspace: workspace}
}

func trustedInvocation(t *testing.T, workspace string, cwd string) state.TrustedInvocation {
	t.Helper()
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: "session-1", WorkspacePath: workspace, Cwd: cwd},
		generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: cwd},
	)
	require.True(t, decision.Allowed)
	return invocation
}

func waitForTerminal(
	t *testing.T,
	store *runner.Store,
	jobID generated.JobID,
	maximumWait time.Duration,
) runner.Snapshot {
	t.Helper()
	deadline := time.NewTimer(maximumWait)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var lastSnapshot runner.Snapshot
	for {
		snapshot, err := store.Load(jobID)
		require.NoError(t, err)
		lastSnapshot = snapshot
		if snapshot.State.Job.Status != generated.JobStatusRunning {
			return snapshot
		}
		select {
		case <-deadline.C:
			dumpLifecycleFailure(t, lastSnapshot)
			t.Fatalf("job %s did not become terminal", jobID)
		case <-ticker.C:
		}
	}
}

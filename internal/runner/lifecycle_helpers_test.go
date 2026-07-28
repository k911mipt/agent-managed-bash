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
	handled, err := runner.DispatchInternal(context.Background(), os.Args[1:])
	if handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
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
	for {
		snapshot, err := store.Load(jobID)
		require.NoError(t, err)
		if snapshot.State.Job.Status != generated.JobStatusRunning {
			return snapshot
		}
		select {
		case <-deadline.C:
			t.Fatalf("job %s did not become terminal", jobID)
		case <-ticker.C:
		}
	}
}

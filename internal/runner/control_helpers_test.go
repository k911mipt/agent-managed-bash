//go:build linux || darwin

package runner_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
)

func newControlManager(t *testing.T) *runner.Manager {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	manager, err := runner.New(runner.Config{
		Executable: executable, StartupTimeout: 3 * time.Second,
		TerminationGrace: 20 * time.Millisecond, PollInterval: 5 * time.Millisecond,
	})
	require.NoError(t, err)
	return manager
}

func trustedInvocationFor(t *testing.T, session generated.SessionID, workspace string) state.TrustedInvocation {
	t.Helper()
	contracts, err := contract.Load()
	require.NoError(t, err)
	invocation, decision := contracts.Policy().BindTrustedInvocation(
		state.HostInvocation{SessionID: session, WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: session, WorkspacePath: workspace, Cwd: workspace},
	)
	require.True(t, decision.Allowed)
	return invocation
}

func startControlJob(
	t *testing.T,
	manager *runner.Manager,
	invocation state.TrustedInvocation,
	command string,
) generated.JobMetadata {
	t.Helper()
	job, err := manager.Start(context.Background(), runner.StartRequest{
		Invocation: invocation, Command: command, HardTimeout: 5 * time.Second, OutputLimitBytes: 1 << 20,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cancelErr := manager.Cancel(context.Background(), runner.CancelRequest{Invocation: invocation, JobID: job.JobID})
		require.True(t, cancelErr == nil || errors.Is(cancelErr, runner.ErrJobNotFound))
	})
	return job
}

func waitCaptured(t *testing.T, invocation state.TrustedInvocation, jobID generated.JobID, captured int) {
	t.Helper()
	contracts, err := contract.Load()
	require.NoError(t, err)
	store, err := runner.OpenStore(invocation, contracts)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	controlWaitForCondition(t, 3*time.Second, func() bool {
		snapshot, loadErr := store.Load(jobID)
		return loadErr == nil && int(snapshot.State.Job.CapturedBytes) >= captured
	})
}

func controlWaitForCondition(t *testing.T, maximumWait time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(maximumWait)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-deadline.C:
			t.Fatal("condition did not become true")
		case <-ticker.C:
		}
	}
}

func observerCursor(snapshot runner.Snapshot, session generated.SessionID) *generated.ObserverCursor {
	for index := range snapshot.State.Observers {
		if snapshot.State.Observers[index].SessionID == session {
			return &snapshot.State.Observers[index]
		}
	}
	return nil
}

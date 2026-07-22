//go:build linux || darwin

package runner_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_Manager_detached_runner_completes_after_caller_process_exits(t *testing.T) {
	// Given
	workspace := t.TempDir()
	releasePath := filepath.Join(workspace, "release")
	metadataPath := filepath.Join(workspace, "job.json")
	executable, err := os.Executable()
	require.NoError(t, err)
	caller := exec.Command(executable, "-test.run=^Test_LifecycleCallerProcess$")
	caller.Env = append(os.Environ(),
		"AMB_CALLER_HELPER=1", "AMB_CALLER_WORKSPACE="+workspace,
		"AMB_CALLER_RELEASE="+releasePath, "AMB_CALLER_METADATA="+metadataPath,
	)

	// When
	require.NoError(t, caller.Run())
	rawMetadata, err := os.ReadFile(metadataPath)
	require.NoError(t, err)
	var job generated.JobMetadata
	require.NoError(t, json.Unmarshal(rawMetadata, &job))
	invocation := trustedInvocation(t, workspace, workspace)
	contracts, err := contract.Load()
	require.NoError(t, err)
	store, err := runner.OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	running, err := store.Load(job.JobID)
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusRunning, running.State.Job.Status)
	matches, err := runner.VerifyProcessIdentity(
		running.Runtime.ProcessGroupLeaderPID,
		running.Runtime.ProcessBirthIdentity,
	)
	require.NoError(t, err)
	require.True(t, matches)
	matches, err = runner.VerifyProcessIdentity(running.Runtime.ProcessGroupLeaderPID, "linux-starttime:wrong")
	require.NoError(t, err)
	require.False(t, matches)
	require.NoError(t, os.WriteFile(releasePath, []byte("release"), 0o600))
	snapshot := waitForTerminal(t, store, job.JobID, testLifecycleDeadline)
	output, err := store.ReadOutput(job.JobID)

	// Then
	require.NoError(t, err)
	require.Equal(t, generated.JobStatusSucceeded, snapshot.State.Job.Status)
	require.Equal(t, "detached", string(output))
}

func Test_LifecycleCallerProcess(t *testing.T) {
	if os.Getenv("AMB_CALLER_HELPER") != "1" {
		return
	}
	workspace := os.Getenv("AMB_CALLER_WORKSPACE")
	releasePath := os.Getenv("AMB_CALLER_RELEASE")
	metadataPath := os.Getenv("AMB_CALLER_METADATA")
	executable, err := os.Executable()
	require.NoError(t, err)
	manager, err := runner.New(runner.Config{
		Executable: executable, StartupTimeout: testStartupTimeout, TerminationGrace: testTerminationGrace,
	})
	require.NoError(t, err)
	command := `while [ ! -f ` + strconv.Quote(releasePath) + ` ]; do sleep 0.01; done; printf detached`
	job, err := manager.Start(context.Background(), runner.StartRequest{
		Invocation: trustedInvocation(t, workspace, workspace), Command: command,
		HardTimeout: 3 * time.Second, OutputLimitBytes: 1 << 20,
	})
	require.NoError(t, err)
	raw, err := json.Marshal(job)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(metadataPath, raw, 0o600))
}

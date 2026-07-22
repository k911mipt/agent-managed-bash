//go:build linux || darwin

package runner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_Store_append_output_commits_only_exact_accepted_prefix(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 5)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })

	// When
	appendResult, err := store.AppendOutput(initial.Job.JobID, []byte("1234567"))
	output, outputErr := store.ReadOutput(initial.Job.JobID)
	snapshot, loadErr := store.Load(initial.Job.JobID)

	// Then
	require.NoError(t, err)
	require.Equal(t, runner.OutputAppend{AcceptedBytes: 5, LimitReached: true}, appendResult)
	require.NoError(t, outputErr)
	require.Equal(t, []byte("12345"), output)
	require.NoError(t, loadErr)
	require.Equal(t, 5, int(snapshot.State.Job.CapturedBytes))
}

func Test_Store_append_output_overwrites_unclaimed_tail_without_exposing_it(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 5)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	outputPath := filepath.Join(jobPath(workspace, initial.Job.JobID), "output.log")
	require.NoError(t, os.WriteFile(outputPath, []byte("stale-tail"), 0o600))

	// When
	appendResult, err := store.AppendOutput(initial.Job.JobID, []byte("abc"))
	output, outputErr := store.ReadOutput(initial.Job.JobID)

	// Then
	require.NoError(t, err)
	require.Equal(t, runner.OutputAppend{AcceptedBytes: 3, LimitReached: false}, appendResult)
	require.NoError(t, outputErr)
	require.Equal(t, []byte("abc"), output)
}

func Test_Store_remove_terminal_job_does_not_follow_unexpected_symlink(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	lease := commitTestJob(t, store, initial)
	require.NoError(t, store.PublishTerminal(initial.Job.JobID, succeededState(initial), lease))
	external := filepath.Join(t.TempDir(), "external")
	require.NoError(t, os.WriteFile(external, []byte("keep"), 0o600))
	require.NoError(t, os.Symlink(external, filepath.Join(jobPath(workspace, initial.Job.JobID), "unexpected")))

	// When
	err := store.RemoveTerminal(initial.Job.JobID)
	content, readErr := os.ReadFile(external)

	// Then
	require.NoError(t, err)
	require.NoError(t, readErr)
	require.Equal(t, []byte("keep"), content)
}

func Test_Store_rejects_output_truncated_below_committed_bytes(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 5)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	_, err := store.AppendOutput(initial.Job.JobID, []byte("12345"))
	require.NoError(t, err)
	outputPath := filepath.Join(jobPath(workspace, initial.Job.JobID), "output.log")
	require.NoError(t, os.Truncate(outputPath, 2))

	// When
	_, readErr := store.ReadOutput(initial.Job.JobID)
	_, appendErr := store.AppendOutput(initial.Job.JobID, []byte("more"))

	// Then
	require.ErrorIs(t, readErr, runner.ErrCorruptState)
	require.ErrorIs(t, appendErr, runner.ErrCorruptState)
}

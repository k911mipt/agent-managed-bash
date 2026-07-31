package cli

import (
	"errors"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_classifyStart_preserves_committed_job_on_durability_uncertainty(t *testing.T) {
	// Given
	metadata := generated.JobMetadata{JobID: "job-1"}
	durability := &runner.CommitDurabilityError{JobID: metadata.JobID, Cause: runner.ErrCommitDurabilityUnknown}

	// When
	warning, problem := classifyStart(generated.ActionRun, metadata, durability)

	// Then
	require.Nil(t, problem)
	require.ErrorIs(t, warning, runner.ErrCommitDurabilityUnknown)
}

func Test_classifyStart_rejects_mismatched_durability_metadata(t *testing.T) {
	// Given
	metadata := generated.JobMetadata{JobID: "job-1"}
	durability := &runner.CommitDurabilityError{JobID: "job-2", Cause: errors.New("uncertain")}

	// When
	_, problem := classifyStart(generated.ActionRun, metadata, durability)

	// Then
	require.NotNil(t, problem)
	require.Equal(t, generated.ErrorCodeInternal, problem.code)
}

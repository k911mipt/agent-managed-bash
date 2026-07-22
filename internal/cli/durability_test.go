package cli

import (
	"errors"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_runResult_preserves_committed_job_on_durability_uncertainty(t *testing.T) {
	// Given
	metadata := generated.JobMetadata{JobID: "job-1"}
	durability := &runner.CommitDurabilityError{JobID: metadata.JobID, Cause: runner.ErrCommitDurabilityUnknown}

	// When
	result, problem := runResult(metadata, durability)

	// Then
	require.Nil(t, problem)
	require.ErrorIs(t, result.warning, runner.ErrCommitDurabilityUnknown)
	response, ok := result.response.(generated.RunResponse)
	require.True(t, ok)
	require.Equal(t, metadata.JobID, response.Result.JobID)
}

func Test_runResult_rejects_mismatched_durability_metadata(t *testing.T) {
	// Given
	metadata := generated.JobMetadata{JobID: "job-1"}
	durability := &runner.CommitDurabilityError{JobID: "job-2", Cause: errors.New("uncertain")}

	// When
	_, problem := runResult(metadata, durability)

	// Then
	require.NotNil(t, problem)
	require.Equal(t, generated.ErrorCodeInternal, problem.code)
}

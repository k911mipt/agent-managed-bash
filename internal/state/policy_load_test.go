package state

import (
	"encoding/json"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_LoadPolicy_validates_checked_in_contract(t *testing.T) {
	// When
	policy := loadTestPolicy(t)

	// Then
	require.Equal(t, ByteCount(104857600), policy.CaptureLimit())
	require.False(t, policy.TTLEnabled())
	require.False(t, policy.RestartReattachmentEnabled())
}

func Test_LoadPolicy_rejects_schema_invalid_document(t *testing.T) {
	// Given
	schema, _ := readTestPolicyDocuments(t)

	// When
	_, err := LoadPolicy(schema, []byte(`{"schema_version":1}`))

	// Then
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPolicyValidation)
}

func Test_LoadPolicy_rejects_semantically_malformed_tables(t *testing.T) {
	schema, rawDocument := readTestPolicyDocuments(t)
	tests := []struct {
		name   string
		mutate func(document *policyDocument)
	}{
		{
			name: "duplicate status",
			mutate: func(document *policyDocument) {
				document.Statuses[len(document.Statuses)-1] = generated.JobStatusRunning
			},
		},
		{
			name: "duplicate transition",
			mutate: func(document *policyDocument) {
				document.Transitions[len(document.Transitions)-1] = document.Transitions[0]
			},
		},
		{
			name: "terminal transition source",
			mutate: func(document *policyDocument) {
				document.Transitions[0].From = generated.JobStatusSucceeded
			},
		},
		{
			name: "swapped access code",
			mutate: func(document *policyDocument) {
				document.Access.SameWorkspaceRead = CodePathSymlink
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var document policyDocument
			require.NoError(t, json.Unmarshal(rawDocument, &document))
			testCase.mutate(&document)
			mutated, err := json.Marshal(document)
			require.NoError(t, err)

			_, err = LoadPolicy(schema, mutated)

			require.ErrorIs(t, err, ErrPolicyValidation)
		})
	}
}

func Test_ExitClasses_map_every_protocol_error_and_successful_process_result(t *testing.T) {
	policy := loadTestPolicy(t)
	require.Equal(t, ExitClassSuccess, policy.SuccessExitClass())
	tests := []struct {
		code generated.ErrorCode
		want generated.ExitClass
	}{
		{generated.ErrorCodeMalformedJson, ExitClassValidation},
		{generated.ErrorCodeInvalidRequest, ExitClassValidation},
		{generated.ErrorCodeIncompatibleVersion, ExitClassValidation},
		{generated.ErrorCodeInvalidRange, ExitClassValidation},
		{generated.ErrorCodeInvalidCursor, ExitClassValidation},
		{generated.ErrorCodeJobNotFound, ExitClassAuthorization},
		{generated.ErrorCodeUnauthorized, ExitClassAuthorization},
		{generated.ErrorCodeActiveJob, ExitClassConflict},
		{generated.ErrorCodeConflict, ExitClassConflict},
		{generated.ErrorCodeCorruptState, ExitClassInternal},
		{generated.ErrorCodeRunnerUnavailable, ExitClassInternal},
		{generated.ErrorCodeIoFailure, ExitClassInternal},
		{generated.ErrorCodeInternal, ExitClassInternal},
	}
	for _, testCase := range tests {
		actual, found := policy.ExitClassForError(testCase.code)
		require.True(t, found)
		require.Equal(t, testCase.want, actual)
	}
	for _, status := range []generated.JobStatus{generated.JobStatusNonzeroExit, generated.JobStatusSignalExit} {
		t.Run(string(status), func(t *testing.T) {
			require.Equal(t, ExitClassSuccess, policy.SuccessExitClass())
		})
	}
	_, found := policy.ExitClassForError(generated.ErrorCode("unknown"))
	require.False(t, found)
}

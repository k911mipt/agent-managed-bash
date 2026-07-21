package state

import (
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_ProtocolOutcomeForCode_maps_every_internal_code(t *testing.T) {
	policy := loadTestPolicy(t)
	tests := []struct {
		code      Code
		kind      ProtocolOutcomeKind
		errorCode generated.ErrorCode
		exitClass generated.ExitClass
	}{
		{CodeAllow, ProtocolOutcomeSuccess, "", ExitClassSuccess},
		{CodeCancellationRequested, ProtocolOutcomeSuccess, "", ExitClassSuccess},
		{CodeCancellationIdempotent, ProtocolOutcomeSuccess, "", ExitClassSuccess},
		{CodeCancellationTerminal, ProtocolOutcomeSuccess, "", ExitClassSuccess},
		{CodeTransitionNotAllowed, ProtocolOutcomeError, generated.ErrorCodeConflict, ExitClassConflict},
		{CodeInvalidStatus, ProtocolOutcomeError, generated.ErrorCodeCorruptState, ExitClassInternal},
		{CodeCorruptState, ProtocolOutcomeError, generated.ErrorCodeCorruptState, ExitClassInternal},
		{CodeJobNotFound, ProtocolOutcomeError, generated.ErrorCodeJobNotFound, ExitClassAuthorization},
		{CodeUnauthorized, ProtocolOutcomeError, generated.ErrorCodeUnauthorized, ExitClassAuthorization},
		{CodeActiveJob, ProtocolOutcomeError, generated.ErrorCodeActiveJob, ExitClassConflict},
		{CodeInvalidCursor, ProtocolOutcomeError, generated.ErrorCodeInvalidCursor, ExitClassValidation},
		{CodeInvalidRange, ProtocolOutcomeError, generated.ErrorCodeInvalidRange, ExitClassValidation},
		{CodePathInvalid, ProtocolOutcomeError, generated.ErrorCodeInvalidRequest, ExitClassValidation},
		{CodePathOutsideWorkspace, ProtocolOutcomeError, generated.ErrorCodeUnauthorized, ExitClassAuthorization},
		{CodePathSymlink, ProtocolOutcomeError, generated.ErrorCodeUnauthorized, ExitClassAuthorization},
		{CodePathUnavailable, ProtocolOutcomeError, generated.ErrorCodeIoFailure, ExitClassInternal},
	}
	for _, testCase := range tests {
		outcome, found := policy.ProtocolOutcomeForCode(testCase.code)
		require.True(t, found, "missing code %q", testCase.code)
		require.Equal(t, testCase.kind, outcome.Kind)
		require.Equal(t, testCase.errorCode, outcome.ErrorCode)
		require.Equal(t, testCase.exitClass, outcome.ExitClass)
	}
	_, found := policy.ProtocolOutcomeForCode(Code("unknown"))
	require.False(t, found)
}

func Test_ProtocolOutcomeForCode_returns_value_detached_from_policy(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)
	outcome, found := policy.ProtocolOutcomeForCode(CodeInvalidRange)
	require.True(t, found)

	// When
	outcome.ErrorCode = generated.ErrorCodeInternal
	outcome.ExitClass = ExitClassInternal
	fresh, found := policy.ProtocolOutcomeForCode(CodeInvalidRange)

	// Then
	require.True(t, found)
	require.Equal(t, ProtocolOutcome{
		Kind: ProtocolOutcomeError, ErrorCode: generated.ErrorCodeInvalidRange, ExitClass: ExitClassValidation,
	}, fresh)
}

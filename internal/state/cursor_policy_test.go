package state

import (
	"encoding/hex"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Cursors_and_ranges_match_policy_cases(t *testing.T) {
	policy := loadTestPolicy(t)
	cases := loadPolicyCases(t)
	for _, testCase := range cases.Cursors {
		t.Run(testCase.Name, func(t *testing.T) {
			decision := policy.ValidateCursor(CursorContext{Cursor: testCase.Cursor, Captured: testCase.Captured})
			require.Equal(t, Decision{Allowed: testCase.Allowed, Code: testCase.Code}, decision)
		})
	}
	for _, testCase := range cases.Ranges {
		t.Run(testCase.Name, func(t *testing.T) {
			decision := policy.ValidateRange(RangeContext{
				Start: testCase.Start, End: testCase.End, Captured: testCase.Captured,
			})
			require.Equal(t, Decision{Allowed: testCase.Allowed, Code: testCase.Code}, decision)
		})
	}
}

func Test_Capture_prefix_matches_policy_cases(t *testing.T) {
	policy := loadTestPolicy(t)
	for _, testCase := range loadPolicyCases(t).Capture {
		t.Run(testCase.Name, func(t *testing.T) {
			accepted, decision := policy.AcceptedBytes(
				ByteCount(testCase.Captured),
				ByteCount(testCase.Incoming),
				policy.CaptureLimit(),
			)
			require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
			require.Equal(t, ByteCount(testCase.Accepted), accepted)
		})
	}

	smallPolicy := policy
	smallPolicy.captureLimit = ByteCount(7)
	prefix, decision := smallPolicy.AcceptedIncomingPrefix(ByteCount(4), []byte("56789"), ByteCount(7))
	require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
	require.Equal(t, []byte("567"), prefix)
	require.Equal(t, len(prefix), cap(prefix))
}

func Test_Capture_rejects_existing_prefix_above_effective_limit(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)

	// When
	prefix, decision := policy.AcceptedIncomingPrefix(ByteCount(8), nil, ByteCount(7))

	// Then
	require.Nil(t, prefix)
	require.Equal(t, Decision{Allowed: false, Code: CodeCorruptState}, decision)
}

func Test_Capture_uses_lower_per_job_limit(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)

	// When
	incoming := []byte("56789")
	prefix, decision := policy.AcceptedIncomingPrefix(ByteCount(4), incoming, ByteCount(6))

	// Then
	require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
	require.Equal(t, []byte("56"), prefix)
	prefix[0] = 'X'
	require.Equal(t, byte('5'), incoming[0])
}

func Test_Output_uses_raw_byte_cursors_before_UTF8_replacement(t *testing.T) {
	policy := loadTestPolicy(t)
	for _, testCase := range loadPolicyCases(t).Output {
		t.Run(testCase.Name, func(t *testing.T) {
			raw, err := hex.DecodeString(testCase.RawHex)
			require.NoError(t, err)
			original := append([]byte(nil), raw...)

			output, decision := policy.Output(raw, OutputContext{
				Range:    RangeContext{Start: testCase.Start, End: testCase.End, Captured: testCase.Captured},
				Terminal: testCase.Terminal,
			})
			require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
			require.Equal(t, testCase.Text, output.Text)
			require.Equal(t, testCase.Start, int64(output.StartCursorBytes))
			require.Equal(t, testCase.Next, int64(output.NextCursorBytes))
			require.Equal(t, testCase.Captured, int64(output.CapturedBytes))
			require.Equal(t, testCase.Eof, output.Eof)
			require.Equal(t, original, raw)
		})
	}
}

func Test_ResolveOutputRange_matches_optional_bound_fixtures(t *testing.T) {
	policy := loadTestPolicy(t)
	for _, testCase := range loadPolicyCases(t).OutputRanges {
		t.Run(testCase.Name, func(t *testing.T) {
			// Given
			payload := generated.OutputPayload{JobID: "job-1"}
			if testCase.Start != nil {
				start := generated.ByteCursor(*testCase.Start)
				payload.StartCursorBytes = &start
			}
			if testCase.End != nil {
				end := generated.ByteCursor(*testCase.End)
				payload.EndCursorBytes = &end
			}

			// When
			resolved, decision := policy.ResolveOutputRange(payload, testCase.Captured)

			// Then
			require.Equal(t, Decision{Allowed: testCase.Allowed, Code: testCase.Code}, decision)
			require.Equal(t, RangeContext{
				Start: testCase.ResolvedStart, End: testCase.ResolvedEnd, Captured: testCase.Captured,
			}, resolved)
		})
	}
}

func Test_Output_reports_eof_only_at_terminal_captured_boundary(t *testing.T) {
	policy := loadTestPolicy(t)
	tests := []struct {
		name     string
		terminal bool
		end      int64
		wantEOF  bool
	}{
		{name: "running at boundary", terminal: false, end: 2, wantEOF: false},
		{name: "terminal before boundary", terminal: true, end: 1, wantEOF: false},
		{name: "terminal at boundary", terminal: true, end: 2, wantEOF: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			output, decision := policy.Output([]byte("ok"), OutputContext{
				Range:    RangeContext{Start: 0, End: testCase.end, Captured: 2},
				Terminal: testCase.terminal,
			})
			require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
			require.Equal(t, testCase.wantEOF, output.Eof)
		})
	}
}

func Test_Output_maps_raw_length_mismatch_to_corrupt_state(t *testing.T) {
	policy := loadTestPolicy(t)

	_, decision := policy.Output([]byte("x"), OutputContext{
		Range:    RangeContext{Start: 0, End: 1, Captured: 2},
		Terminal: true,
	})

	require.Equal(t, Decision{Allowed: false, Code: CodeCorruptState}, decision)
}

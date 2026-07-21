package state

import (
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_ObserverPolicy_matches_defaults_and_advancement_fixtures(t *testing.T) {
	policy := loadTestPolicy(t)
	for _, testCase := range loadPolicyCases(t).Observers {
		t.Run(testCase.Name, func(t *testing.T) {
			var explicit *generated.ByteCursor
			if testCase.Explicit != nil {
				value := generated.ByteCursor(*testCase.Explicit)
				explicit = &value
			}
			var observer *generated.ObserverCursor
			if testCase.Persisted != nil {
				observer = &generated.ObserverCursor{SessionID: "observer", CursorBytes: generated.ByteCursor(*testCase.Persisted)}
			}
			resolved := policy.ResolveWaitCursor(WaitCursorContext{Explicit: explicit, Observer: observer})
			require.Equal(t, testCase.Resolved, int64(resolved))

			current := generated.ObserverCursor{SessionID: "observer", CursorBytes: generated.ByteCursor(testCase.Resolved)}
			after, decision := policy.ObserverAfter(ObserverAdvanceContext{
				Action:          testCase.Action,
				Current:         current,
				Output:          generated.OutputChunk{NextCursorBytes: generated.ByteCursor(testCase.Next), CapturedBytes: generated.ByteCursor(testCase.Next)},
				UpdatedAtUnixMs: 1010,
			})
			require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
			require.Equal(t, testCase.Advanced, int64(after.CursorBytes))
			require.Equal(t, testCase.ShouldMove, after.CursorBytes != current.CursorBytes)
		})
	}
}

func Test_Policy_exposes_frozen_MVP_defaults(t *testing.T) {
	policy := loadTestPolicy(t)

	require.Equal(t, generated.TimeoutMs(300000), policy.WaitTimeout())
	require.Equal(t, generated.TimeoutMs(120000), policy.IdleCheckpoint())
	require.Equal(t, generated.TimeoutMs(7200000), policy.HardTimeout())
	require.Equal(t, ByteCount(104857600), policy.CaptureLimit())
}

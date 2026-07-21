package state

import (
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Transitions_accept_every_listed_and_reject_every_unlisted_pair(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)
	statuses := policy.Statuses()
	require.Len(t, statuses, 8)
	for _, status := range statuses {
		require.Equal(t, status != generated.JobStatusRunning, policy.isTerminal(status))
	}

	for _, from := range statuses {
		for _, to := range statuses {
			name := string(from) + "_to_" + string(to)
			t.Run(name, func(t *testing.T) {
				// When
				decision := policy.AuthorizeTransition(from, to)

				// Then
				expected := from == generated.JobStatusRunning && to != generated.JobStatusRunning
				require.Equal(t, expected, decision.Allowed)
				if expected {
					require.Equal(t, CodeAllow, decision.Code)
				} else {
					require.Equal(t, CodeTransitionNotAllowed, decision.Code)
				}
			})
		}
	}
}

func Test_Transitions_match_every_legal_fixture(t *testing.T) {
	policy := loadTestPolicy(t)
	fixtures := loadPolicyCases(t).Transitions
	require.Len(t, fixtures, 7)
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			decision := policy.AuthorizeTransition(fixture.From, fixture.To)
			require.Equal(t, Decision{Allowed: fixture.Allowed, Code: fixture.Code}, decision)
		})
	}
}

func Test_Transitions_reject_unknown_status_with_machine_code(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)

	// When
	decision := policy.AuthorizeTransition(generated.JobStatus("paused"), generated.JobStatusSucceeded)

	// Then
	require.Equal(t, Decision{Allowed: false, Code: CodeInvalidStatus}, decision)
}

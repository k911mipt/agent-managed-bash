package state

import (
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Authorization_matches_policy_cases(t *testing.T) {
	policy := loadTestPolicy(t)
	for _, testCase := range loadPolicyCases(t).Authorization {
		t.Run(testCase.Name, func(t *testing.T) {
			// Given
			access := AccessContext{
				JobWorkspace:     "/workspace-a",
				RequestWorkspace: "/workspace-a",
				OwnerSession:     generated.SessionID("owner"),
				ActorSession:     generated.SessionID("other"),
			}
			if !testCase.SameWorkspace {
				access.RequestWorkspace = "/workspace-b"
			}
			if testCase.Owner {
				access.ActorSession = access.OwnerSession
			}

			// When
			var decision Decision
			switch testCase.Operation {
			case "read":
				decision = policy.AuthorizeRead(access)
			case "mutate":
				decision = policy.AuthorizeMutation(access)
			default:
				t.Fatalf("unknown operation %q", testCase.Operation)
			}

			// Then
			require.Equal(t, Decision{Allowed: testCase.Allowed, Code: testCase.Code}, decision)
		})
	}
}

func Test_Removal_matches_policy_cases(t *testing.T) {
	policy := loadTestPolicy(t)
	for _, testCase := range loadPolicyCases(t).Removal {
		t.Run(testCase.Name, func(t *testing.T) {
			decision := policy.AuthorizeRemoval(testCase.Status)
			require.Equal(t, Decision{Allowed: testCase.Allowed, Code: testCase.Code}, decision)
		})
	}
}

func Test_Cancellation_matches_policy_cases(t *testing.T) {
	policy := loadTestPolicy(t)
	for _, testCase := range loadPolicyCases(t).Cancellation {
		t.Run(testCase.Name, func(t *testing.T) {
			decision := policy.EvaluateCancellation(CancellationContext{
				Status: testCase.Status, AlreadyRequested: testCase.AlreadyRequested,
			})
			require.Equal(t, testCase.Allowed, decision.Allowed)
			require.Equal(t, testCase.Code, decision.Code)
			require.Equal(t, testCase.PersistRequest, decision.PersistRequest)
		})
	}
}

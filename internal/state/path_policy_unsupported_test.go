//go:build !linux && !darwin

package state

import (
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Descriptor_bound_paths_fail_closed_on_unsupported_platforms(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)
	host := HostInvocation{SessionID: "session-1", WorkspacePath: "/workspace", Cwd: "/workspace"}
	asserted := generated.TrustedContext{
		SessionID: "session-1", WorkspacePath: "/workspace", Cwd: "/workspace",
	}

	// When
	file, openDecision := policy.OpenWorkspacePath("/workspace", "/workspace/file")
	_, bindDecision := policy.BindTrustedInvocation(host, asserted)

	// Then
	require.Nil(t, file)
	require.Equal(t, Decision{Allowed: false, Code: CodePathUnavailable}, openDecision)
	require.Equal(t, Decision{Allowed: false, Code: CodePathUnavailable}, bindDecision)
}

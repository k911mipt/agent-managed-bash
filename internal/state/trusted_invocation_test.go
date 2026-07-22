package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_BindTrustedInvocation_rejects_forged_request_assertions(t *testing.T) {
	policy := loadTestPolicy(t)
	workspace := filepath.Join(t.TempDir(), "workspace")
	cwd := filepath.Join(workspace, "sub")
	require.NoError(t, os.MkdirAll(cwd, 0o700))
	host := HostInvocation{SessionID: "owner", WorkspacePath: workspace, Cwd: cwd}
	tests := []generated.TrustedContext{
		{SessionID: "attacker", WorkspacePath: workspace, Cwd: cwd},
		{SessionID: "owner", WorkspacePath: filepath.Join(workspace, "other"), Cwd: cwd},
		{SessionID: "owner", WorkspacePath: workspace, Cwd: filepath.Join(workspace, "other")},
	}
	for _, asserted := range tests {
		_, decision := policy.BindTrustedInvocation(host, asserted)
		require.Equal(t, Decision{Allowed: false, Code: CodeUnauthorized}, decision)
	}
}

func Test_TrustedInvocation_authorizes_owner_and_rejects_non_owner_cancellation(t *testing.T) {
	policy := loadTestPolicy(t)
	job := validRunningState().Job
	workspace := filepath.Join(t.TempDir(), "workspace")
	require.NoError(t, os.Mkdir(workspace, 0o700))
	job.WorkspacePath = workspace
	job.Cwd = workspace
	asserted := generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace}
	owner, decision := policy.BindTrustedInvocation(
		HostInvocation{SessionID: "session-1", WorkspacePath: workspace, Cwd: workspace},
		asserted,
	)
	require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
	require.True(t, policy.EvaluateTrustedCancellation(job, false, owner).Allowed)

	nonOwner, decision := policy.BindTrustedInvocation(
		HostInvocation{SessionID: "session-2", WorkspacePath: workspace, Cwd: workspace},
		generated.TrustedContext{SessionID: "session-2", WorkspacePath: workspace, Cwd: workspace},
	)
	require.Equal(t, Decision{Allowed: true, Code: CodeAllow}, decision)
	require.Equal(t, CodeUnauthorized, policy.EvaluateTrustedCancellation(job, false, nonOwner).Code)
}

func Test_BindTrustedInvocation_descriptor_validates_workspace_and_nested_cwd(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	cwd := filepath.Join(workspace, "nested", "cwd")
	require.NoError(t, os.MkdirAll(cwd, 0o700))
	workspaceLink := filepath.Join(base, "workspace-link")
	require.NoError(t, os.Symlink(workspace, workspaceLink))
	cwdLink := filepath.Join(workspace, "cwd-link")
	require.NoError(t, os.Symlink(cwd, cwdLink))

	tests := []struct {
		name      string
		workspace string
		cwd       string
		expected  Decision
	}{
		{name: "real directories", workspace: workspace, cwd: cwd, expected: Decision{Allowed: true, Code: CodeAllow}},
		{
			name: "symlinked workspace", workspace: workspaceLink,
			cwd:      filepath.Join(workspaceLink, "nested", "cwd"),
			expected: Decision{Allowed: false, Code: CodePathSymlink},
		},
		{
			name: "symlinked cwd", workspace: workspace, cwd: cwdLink,
			expected: Decision{Allowed: false, Code: CodePathSymlink},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// When
			_, decision := policy.BindTrustedInvocation(
				HostInvocation{SessionID: "session-1", WorkspacePath: testCase.workspace, Cwd: testCase.cwd},
				generated.TrustedContext{SessionID: "session-1", WorkspacePath: testCase.workspace, Cwd: testCase.cwd},
			)

			// Then
			require.Equal(t, testCase.expected, decision)
		})
	}
}

func Test_TrustedInvocation_exposes_read_only_bound_values(t *testing.T) {
	// Given
	policy := loadTestPolicy(t)
	workspace := filepath.Join(t.TempDir(), "workspace")
	cwd := filepath.Join(workspace, "cwd")
	require.NoError(t, os.MkdirAll(cwd, 0o700))
	invocation, decision := policy.BindTrustedInvocation(
		HostInvocation{SessionID: "session-1", WorkspacePath: workspace, Cwd: cwd},
		generated.TrustedContext{SessionID: "session-1", WorkspacePath: workspace, Cwd: cwd},
	)
	require.True(t, decision.Allowed)

	// When
	sessionID := invocation.SessionID()
	boundWorkspace := invocation.WorkspacePath()
	boundCwd := invocation.Cwd()

	// Then
	require.Equal(t, generated.SessionID("session-1"), sessionID)
	require.Equal(t, workspace, boundWorkspace)
	require.Equal(t, cwd, boundCwd)
}

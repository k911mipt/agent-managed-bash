package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Application_list_rejects_missing_host_context(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	workspace := t.TempDir()
	useWorkingDirectory(t, workspace)
	t.Setenv(testHostSessionEnvironment, "")
	t.Setenv(testHostWorkspaceEnvironment, "")
	request := fmt.Sprintf(
		`{"schema_version":1,"action":"list","context":{"session_id":"session-1","workspace_path":%q,"cwd":%q}}`,
		workspace,
		workspace,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	exitCode := application.Execute(context.Background(), []string{"list"}, Streams{
		Stdin: bytes.NewBufferString(request), Stdout: &stdout, Stderr: &stderr,
	})

	// Then
	require.Equal(t, 3, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"action":"list","error":{"code":"unauthorized","message":"request is unauthorized"}}`, stdout.String())
	require.Contains(t, stderr.String(), "action=list code=unauthorized")
}

func Test_Application_list_rejects_forged_request_context(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	workspace := t.TempDir()
	useWorkingDirectory(t, workspace)
	request := fmt.Sprintf(
		`{"schema_version":1,"action":"list","context":{"session_id":"attacker","workspace_path":%q,"cwd":%q}}`,
		workspace,
		workspace,
	)

	// When
	exitCode, stdout, stderr := (testClient{
		application: application, workspace: workspace, session: "owner",
	}).execute(t, "list", []byte(request))

	// Then
	require.Equal(t, 3, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"action":"list","error":{"code":"unauthorized","message":"request is unauthorized","details":{"field":"context.session_id","expected":"trusted host session","actual":"attacker"}}}`, stdout)
	require.Contains(t, stderr, "action=list code=unauthorized")
	_, sessionPresent := os.LookupEnv(testHostSessionEnvironment)
	_, workspacePresent := os.LookupEnv(testHostWorkspaceEnvironment)
	require.False(t, sessionPresent)
	require.False(t, workspacePresent)
}

func Test_Application_list_reports_invalid_root_workspace(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	cwd := t.TempDir()
	useWorkingDirectory(t, cwd)
	request := fmt.Sprintf(
		`{"schema_version":1,"action":"list","context":{"session_id":"session-1","workspace_path":"/","cwd":%q}}`,
		cwd,
	)

	// When
	exitCode, stdout, stderr := (testClient{
		application: application, workspace: "/", session: "session-1",
	}).execute(t, "list", []byte(request))

	// Then
	require.Equal(t, 2, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"action":"list","error":{"code":"invalid_request","message":"request is invalid","details":{"field":"context.workspace_path","expected":"physical canonical workspace directory other than /","actual":"/"}}}`, stdout)
	require.Contains(t, stderr, "action=list code=invalid_request")
}

func Test_Application_missing_host_context_precedes_unavailable_cwd(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	previous, err := os.Getwd()
	require.NoError(t, err)
	deadCwd := filepath.Join(t.TempDir(), "removed")
	require.NoError(t, os.Mkdir(deadCwd, 0o700))
	require.NoError(t, os.Chdir(deadCwd))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
	require.NoError(t, os.Remove(deadCwd))
	t.Setenv(testHostSessionEnvironment, "")
	t.Setenv(testHostWorkspaceEnvironment, "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	exitCode := application.Execute(context.Background(), []string{"list"}, Streams{
		Stdin:  bytes.NewBufferString(`{"schema_version":1,"action":"list","context":{"session_id":"session-1","workspace_path":"/workspace","cwd":"/workspace"}}`),
		Stdout: &stdout,
		Stderr: &stderr,
	})

	// Then
	require.Equal(t, 3, exitCode)
	require.Contains(t, stdout.String(), `"code":"unauthorized"`)
}

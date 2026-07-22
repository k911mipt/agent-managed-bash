package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

const (
	testHostSessionEnvironment   = "MANAGED_BASH_HOST_SESSION_ID"
	testHostWorkspaceEnvironment = "MANAGED_BASH_HOST_WORKSPACE_PATH"
)

func TestMain(m *testing.M) {
	handled, err := runner.DispatchInternal(context.Background(), os.Args[1:])
	if handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func useWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(path))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
}

type testClient struct {
	application *Application
	workspace   string
	session     string
}

func (client testClient) execute(t *testing.T, action string, request []byte) (int, string, string) {
	t.Helper()
	t.Setenv(testHostSessionEnvironment, client.session)
	t.Setenv(testHostWorkspaceEnvironment, client.workspace)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := client.application.Execute(context.Background(), []string{action}, Streams{
		Stdin: bytes.NewReader(request), Stdout: &stdout, Stderr: &stderr,
	})
	return exitCode, stdout.String(), stderr.String()
}

func marshalRequest(t *testing.T, request any) []byte {
	t.Helper()
	raw, err := json.Marshal(request)
	require.NoError(t, err)
	return raw
}

package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/cli"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_run_executes_public_version_command(t *testing.T) {
	// Given
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	streams := cli.Streams{
		Stdin:  bytes.NewBufferString(`{"schema_version":1,"action":"version"}`),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	// When
	exitCode := run([]string{"version"}, streams)

	// Then
	require.Equal(t, 0, exitCode)
	require.Empty(t, stderr.String())
	var response generated.VersionResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &response))
	require.Equal(t, "managed-bash", response.Result.Product)
	require.Equal(t, binaryVersion, response.Result.BinaryVersion)
}

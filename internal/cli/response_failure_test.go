package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Application_converts_invalid_success_response_to_structured_internal_error(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	exitCode := application.writeSuccess(Streams{Stdout: &stdout, Stderr: &stderr}, generated.ActionVersion, struct{}{})

	// Then
	require.Equal(t, 5, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"action":"version","error":{"code":"internal","message":"internal failure"}}`, stdout.String())
	require.Contains(t, stderr.String(), "action=version code=internal")
}

func Test_Application_does_not_write_second_response_after_stdout_failure(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	writer := &recordingErrorWriter{err: errors.New("broken stdout")}
	var stderr bytes.Buffer

	// When
	exitCode := application.writeSuccess(
		Streams{Stdout: writer, Stderr: &stderr},
		generated.ActionVersion,
		application.versionResponse(),
	)

	// Then
	require.Equal(t, 5, exitCode)
	require.Equal(t, 1, writer.calls)
	require.Contains(t, stderr.String(), "code=io_failure")
}

type recordingErrorWriter struct {
	err   error
	calls int
}

func (writer *recordingErrorWriter) Write([]byte) (int, error) {
	writer.calls++
	return 0, writer.err
}

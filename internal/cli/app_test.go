package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Application_version_writes_golden_response(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "0.1.0"})
	require.NoError(t, err)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	streams := Streams{
		Stdin:  strings.NewReader(`{"schema_version":1,"action":"version"}`),
		Stdout: &stdout,
		Stderr: &stderr,
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "version.golden.json"))
	require.NoError(t, err)
	expected := strings.ReplaceAll(string(golden), "${GOOS}", runtime.GOOS)
	expected = strings.ReplaceAll(expected, "${GOARCH}", runtime.GOARCH)

	// When
	exitCode := application.Execute(context.Background(), []string{"version"}, streams)

	// Then
	require.Equal(t, 0, exitCode)
	require.Equal(t, expected, stdout.String())
	require.Empty(t, stderr.String())
}

func Test_Application_returns_malformed_json_before_action_validation(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	streams := Streams{
		Stdin:  strings.NewReader(`{"schema_version":1`),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	// When
	exitCode := application.Execute(context.Background(), []string{"run"}, streams)

	// Then
	require.Equal(t, 2, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"error":{"code":"malformed_json","message":"request is not JSON"}}`, stdout.String())
	require.Contains(t, stderr.String(), "action=run code=malformed_json")
	require.NotContains(t, stderr.String(), `{"schema_version"`)
}

func Test_Application_returns_incompatible_version_for_integer_version(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	streams := Streams{
		Stdin:  strings.NewReader(`{"schema_version":2,"action":"version"}`),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	// When
	exitCode := application.Execute(context.Background(), []string{"version"}, streams)

	// Then
	require.Equal(t, 2, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"action":"version","error":{"code":"incompatible_version","message":"protocol version is incompatible","details":{"field":"schema_version","expected":"1","actual":"2"}}}`, stdout.String())
	require.Contains(t, stderr.String(), "action=version code=incompatible_version")
}

func Test_Application_rejects_action_that_differs_from_subcommand(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	streams := Streams{
		Stdin:  strings.NewReader(`{"schema_version":1,"action":"status","context":{"session_id":"session-1","workspace_path":"/workspace","cwd":"/workspace"},"payload":{"job_id":"job-1"}}`),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	// When
	exitCode := application.Execute(context.Background(), []string{"output"}, streams)

	// Then
	require.Equal(t, 2, exitCode)
	require.JSONEq(t, `{"schema_version":1,"ok":false,"action":"status","error":{"code":"invalid_request","message":"request is invalid","details":{"field":"action","expected":"output","actual":"status"}}}`, stdout.String())
	require.Contains(t, stderr.String(), "action=status code=invalid_request")
}

func Test_Application_help_prints_usage_without_reading_stdin(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	exitCode := application.Execute(context.Background(), []string{"--help"}, Streams{
		Stdin: strings.NewReader("not JSON"), Stdout: &stdout, Stderr: &stderr,
	})

	// Then
	require.Equal(t, 0, exitCode)
	require.Equal(t, "usage: managed-bash <start|run|wait|status|output|cancel|remove|list|version>\n", stdout.String())
	require.Empty(t, stderr.String())
}

func Test_Application_rejects_terminal_stdin_without_reading_request(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	streams := Streams{
		Stdin: strings.NewReader(`{"schema_version":1,"action":"list"}`), Stdout: &stdout, Stderr: &stderr,
		StdinIsTerminal: true,
	}

	// When
	exitCode := application.Execute(context.Background(), []string{"list"}, streams)

	// Then
	require.Equal(t, 2, exitCode)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "requires a JSON request on stdin")
	require.Contains(t, stderr.String(), usage)
}

func Test_Application_accepts_schema_version_numeric_integer_forms(t *testing.T) {
	for _, version := range []string{"1.0", "1e0"} {
		t.Run(version, func(t *testing.T) {
			// Given
			application, err := New(Config{BinaryVersion: "dev"})
			require.NoError(t, err)
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			// When
			exitCode := application.Execute(context.Background(), []string{"version"}, Streams{
				Stdin:  strings.NewReader(`{"schema_version":` + version + `,"action":"version"}`),
				Stdout: &stdout,
				Stderr: &stderr,
			})

			// Then
			require.Equal(t, 0, exitCode)
			require.Empty(t, stderr.String())
		})
	}
}

func Test_Application_returns_incompatible_version_for_arbitrary_precision_integer(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	exitCode := application.Execute(context.Background(), []string{"version"}, Streams{
		Stdin:  strings.NewReader(`{"schema_version":9223372036854775808,"action":"version"}`),
		Stdout: &stdout,
		Stderr: &stderr,
	})

	// Then
	require.Equal(t, 2, exitCode)
	require.Contains(t, stdout.String(), `"code":"incompatible_version"`)
}

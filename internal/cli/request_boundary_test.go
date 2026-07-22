package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Application_rejects_bounded_request_framing_errors(t *testing.T) {
	tests := []struct {
		name     string
		request  []byte
		expected string
	}{
		{name: "invalid UTF-8", request: []byte{0xff}, expected: "malformed_json"},
		{
			name:     "trailing JSON",
			request:  []byte(`{"schema_version":1,"action":"version"}{"schema_version":1,"action":"version"}`),
			expected: "malformed_json",
		},
		{name: "oversize", request: []byte(strings.Repeat(" ", maxRequestBytes+1)), expected: "invalid_request"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Given
			application, err := New(Config{BinaryVersion: "dev"})
			require.NoError(t, err)
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			// When
			exitCode := application.Execute(context.Background(), []string{"version"}, Streams{
				Stdin: bytes.NewReader(testCase.request), Stdout: &stdout, Stderr: &stderr,
			})

			// Then
			require.Equal(t, 2, exitCode)
			require.Contains(t, stdout.String(), `"code":"`+testCase.expected+`"`)
		})
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Application_writes_golden_fixtures_for_every_action_and_error_code(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	fixtureRoot := filepath.Join("..", "..", "fixtures", "v1", "schema", "valid")
	fixtures := []string{
		"response-run.json", "response-wait.json", "response-status.json", "response-output.json",
		"response-cancel.json", "response-remove.json", "response-list.json", "response-version.json",
		"error-malformed-json.json", "error-validation.json", "error-incompatible-version.json",
		"error-invalid-range.json", "error-invalid-cursor.json", "error-not-found.json",
		"error-unauthorized.json", "error-conflict.json", "error-generic-conflict.json",
		"error-corrupt-state.json", "error-runner-unavailable.json", "error-io-failure.json", "error-internal.json",
	}

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(fixtureRoot, fixture))
			require.NoError(t, err)
			var expected bytes.Buffer
			require.NoError(t, json.Compact(&expected, raw))
			require.NoError(t, expected.WriteByte('\n'))
			var actual bytes.Buffer

			// When
			err = application.writeResponse(&actual, json.RawMessage(raw))

			// Then
			require.NoError(t, err)
			require.Equal(t, expected.String(), actual.String())
		})
	}
}

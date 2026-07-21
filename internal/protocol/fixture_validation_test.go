package protocol_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_ProtocolSchemaFixtures_match_manifest(t *testing.T) {
	// Given
	repositoryRoot := filepath.Join("..", "..")
	manifest := loadFixtureManifest(t, repositoryRoot)
	schemas := compileProtocolSchemas(t, repositoryRoot)

	for _, fixture := range manifest.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			schema, exists := schemas[fixture.Schema]
			require.True(t, exists, "unknown root schema %q", fixture.Schema)

			rawFixture, err := os.ReadFile(filepath.Join(repositoryRoot, "fixtures", "v1", "schema", fixture.Path))
			require.NoError(t, err)

			// When
			document, parseErr := parseFixtureJSON(rawFixture)
			var validationErr error
			if parseErr == nil {
				validationErr = schema.Validate(document)
			}
			valid := parseErr == nil && validationErr == nil

			// Then
			require.Equal(
				t,
				fixture.Valid,
				valid,
				"parse error: %v; validation error: %v",
				parseErr,
				validationErr,
			)
		})
	}
}

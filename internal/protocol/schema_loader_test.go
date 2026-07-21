package protocol_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

const schemaBaseURL = "https://agent-managed-bash.dev/schema/v1/"

var schemaFiles = map[string]string{
	"models":   "models.schema.json",
	"request":  "request.schema.json",
	"response": "response.schema.json",
	"state":    "state.schema.json",
}

func compileProtocolSchemas(t *testing.T, repositoryRoot string) map[string]*jsonschema.Schema {
	t.Helper()

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)

	for _, fileName := range schemaFiles {
		rawSchema, err := os.ReadFile(filepath.Join(repositoryRoot, "schemas", "v1", fileName))
		require.NoError(t, err)

		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawSchema))
		require.NoError(t, err)
		require.NoError(t, compiler.AddResource(schemaBaseURL+fileName, document))
	}

	compiled := make(map[string]*jsonschema.Schema, len(schemaFiles))
	for name, fileName := range schemaFiles {
		schema, err := compiler.Compile(schemaBaseURL + fileName)
		require.NoError(t, err)
		compiled[name] = schema
	}

	return compiled
}

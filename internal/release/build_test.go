package release

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_readSharedPayloads_rejects_plugin_release_mismatch(t *testing.T) {
	// Given
	root := t.TempDir()
	sources := map[string]string{
		"LICENSE":                           "license",
		"README.md":                         "readme",
		"packaging/THIRD_PARTY_NOTICES.txt": "notices",
		"packaging/install.sh":              "#!/bin/sh\n",
		"packaging/uninstall.sh":            "#!/bin/sh\n",
		"plugins/opencode/dist/managed-bash.js": `
export function ManagedBashPlugin() {}
Object.defineProperty(ManagedBashPlugin, "managedBashReleaseVersion", { value: "0.2.0" })
`,
	}
	for path, contents := range sources {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte(contents), 0o644))
	}

	// When
	_, err := readSharedPayloads(context.Background(), root, "0.1.0")

	// Then
	require.ErrorIs(t, err, ErrInvalidArchive)
}

//go:build linux || darwin

package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Install_refuses_legacy_registration_created_after_preflight(t *testing.T) {
	tests := []struct {
		name string
		make func(*testing.T, string) string
	}{
		{"regular file", writeForeignFile},
		{"foreign symlink", writeForeignSymlink},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			layout := newTestLayout(t)
			bundle := writeTestBundle(t, "0.1.0", "first")
			var want string

			// When
			err := installWithHooks(context.Background(), layout.config(bundle, "0.1.0"), hooks{
				verifyInstalled: func(context.Context, string, identity) error {
					require.NoError(t, os.MkdirAll(filepath.Dir(layout.legacyPluginLink), 0o755))
					want = test.make(t, layout.legacyPluginLink)
					return nil
				},
			})

			// Then
			require.ErrorIs(t, err, ErrForeignPath)
			requireForeignRegistration(t, layout.legacyPluginLink, want)
			require.NoFileExists(t, filepath.Join(layout.dataRoot, "current"))
			require.NoFileExists(t, layout.binLink)
			require.NoDirExists(t, filepath.Join(layout.dataRoot, "releases", "0.1.0-"+hostTarget()))
		})
	}
}

func Test_Uninstall_refuses_registration_replaced_after_preflight(t *testing.T) {
	tests := []struct {
		name       string
		path       func(testLayout) string
		binaryPath bool
	}{
		{"binary", func(layout testLayout) string { return layout.binLink }, true},
		{"legacy plugin", func(layout testLayout) string { return layout.legacyPluginLink }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			layout := newTestLayout(t)
			bundle := writeTestBundle(t, "0.1.0", "first")
			require.NoError(t, Install(context.Background(), layout.config(bundle, "0.1.0")))
			writeOwnedLegacyPluginLink(t, layout)
			path := test.path(layout)
			const want = "racer"

			// When
			err := uninstallWithHooks(context.Background(), layout.config("", "0.1.0"), hooks{
				beforeUninstallCommit: func() {
					require.NoError(t, os.Remove(path))
					require.NoError(t, os.WriteFile(path, []byte(want), 0o600))
				},
			})

			// Then
			require.ErrorIs(t, err, ErrForeignPath)
			requireForeignRegistration(t, path, want)
			if test.binaryPath {
				requireOwnedLegacyPluginLink(t, layout)
			} else {
				requireOwnedBinaryLink(t, layout)
			}
			require.Equal(t, "releases/0.1.0-"+hostTarget(), currentTarget(t, layout.dataRoot))
		})
	}
}

func requireForeignRegistration(t *testing.T, path string, want string) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	if info.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(path)
		require.NoError(t, readErr)
		require.Equal(t, want, target)
		return
	}
	contents, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, want, string(contents))
}

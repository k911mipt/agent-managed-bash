//go:build linux || darwin

package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Install_publishes_fresh_release_and_binary_registration(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")

	// When
	err := Install(context.Background(), layout.config(bundle, "0.1.0"))

	// Then
	require.NoError(t, err)
	require.Equal(t, "releases/0.1.0-"+hostTarget(), currentTarget(t, layout.dataRoot))
	requireOwnedBinaryLink(t, layout)
	require.NoFileExists(t, layout.legacyPluginLink)
	releaseRoot := filepath.Join(layout.dataRoot, currentTarget(t, layout.dataRoot))
	require.FileExists(t, filepath.Join(releaseRoot, "bin", "managed-bash"))
	require.FileExists(t, filepath.Join(releaseRoot, "lib", "opencode", "managed-bash.js"))
	info, err := os.Stat(releaseRoot)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o555), info.Mode().Perm())
}

func Test_Install_identical_reinstall_is_pointer_noop(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "same")
	require.NoError(t, Install(context.Background(), layout.config(bundle, "0.1.0")))
	before, err := os.Lstat(filepath.Join(layout.dataRoot, "current"))
	require.NoError(t, err)

	// When
	err = Install(context.Background(), layout.config(bundle, "0.1.0"))

	// Then
	require.NoError(t, err)
	after, err := os.Lstat(filepath.Join(layout.dataRoot, "current"))
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
}

func Test_Install_identical_reinstall_removes_owned_legacy_plugin_registration(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "same")
	require.NoError(t, Install(context.Background(), layout.config(bundle, "0.1.0")))
	writeOwnedLegacyPluginLink(t, layout)

	// When
	err := Install(context.Background(), layout.config(bundle, "0.1.0"))

	// Then
	require.NoError(t, err)
	require.NoFileExists(t, layout.legacyPluginLink)
}

func Test_Install_update_switches_only_current_pointer(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	first := writeTestBundle(t, "0.1.0", "first")
	second := writeTestBundle(t, "0.2.0", "second")
	require.NoError(t, Install(context.Background(), layout.config(first, "0.1.0")))
	binBefore, err := os.Lstat(layout.binLink)
	require.NoError(t, err)

	// When
	err = Install(context.Background(), layout.config(second, "0.2.0"))

	// Then
	require.NoError(t, err)
	require.Equal(t, "releases/0.2.0-"+hostTarget(), currentTarget(t, layout.dataRoot))
	binAfter, err := os.Lstat(layout.binLink)
	require.NoError(t, err)
	require.True(t, os.SameFile(binBefore, binAfter))
	require.NoFileExists(t, layout.legacyPluginLink)
}

func Test_Install_precommit_failure_keeps_old_pointer(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	first := writeTestBundle(t, "0.1.0", "first")
	second := writeTestBundle(t, "0.2.0", "second")
	require.NoError(t, Install(context.Background(), layout.config(first, "0.1.0")))
	injected := errors.New("injected precommit failure")

	// When
	err := installWithHooks(context.Background(), layout.config(second, "0.2.0"), hooks{
		beforeCommit: func() error { return injected },
	})

	// Then
	require.ErrorIs(t, err, injected)
	require.Equal(t, "releases/0.1.0-"+hostTarget(), currentTarget(t, layout.dataRoot))
	require.NoDirExists(t, filepath.Join(layout.dataRoot, "releases", "0.2.0-"+hostTarget()))
}

func Test_Install_postswitch_verification_failure_restores_old_pointer(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	first := writeTestBundle(t, "0.1.0", "first")
	second := writeTestBundle(t, "0.2.0", "second")
	require.NoError(t, Install(context.Background(), layout.config(first, "0.1.0")))
	writeOwnedLegacyPluginLink(t, layout)
	injected := errors.New("injected installed verification failure")

	// When
	err := installWithHooks(context.Background(), layout.config(second, "0.2.0"), hooks{
		verifyInstalled: func(context.Context, string, identity) error { return injected },
	})

	// Then
	require.ErrorIs(t, err, injected)
	require.Equal(t, "releases/0.1.0-"+hostTarget(), currentTarget(t, layout.dataRoot))
	requireOwnedLegacyPluginLink(t, layout)
	require.NoDirExists(t, filepath.Join(layout.dataRoot, "releases", "0.2.0-"+hostTarget()))
}

func Test_Install_legacy_cleanup_failure_restores_old_pointer(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	first := writeTestBundle(t, "0.1.0", "first")
	second := writeTestBundle(t, "0.2.0", "second")
	require.NoError(t, Install(context.Background(), layout.config(first, "0.1.0")))
	writeOwnedLegacyPluginLink(t, layout)
	pluginDirectory := filepath.Dir(layout.legacyPluginLink)
	t.Cleanup(func() { require.NoError(t, os.Chmod(pluginDirectory, 0o755)) })

	// When
	err := installWithHooks(context.Background(), layout.config(second, "0.2.0"), hooks{
		beforeLinkCleanup: func(path string) error {
			if path == layout.legacyPluginLink {
				return os.Chmod(pluginDirectory, 0o555)
			}
			return nil
		},
	})

	// Then
	require.Error(t, err)
	require.Equal(t, "releases/0.1.0-"+hostTarget(), currentTarget(t, layout.dataRoot))
	requireOwnedLegacyPluginLink(t, layout)
	require.NoDirExists(t, filepath.Join(layout.dataRoot, "releases", "0.2.0-"+hostTarget()))
}

func Test_Install_postunlink_cleanup_failure_restores_legacy_link(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	first := writeTestBundle(t, "0.1.0", "first")
	second := writeTestBundle(t, "0.2.0", "second")
	require.NoError(t, Install(context.Background(), layout.config(first, "0.1.0")))
	writeOwnedLegacyPluginLink(t, layout)
	injected := errors.New("injected post-unlink cleanup failure")

	// When
	err := installWithHooks(context.Background(), layout.config(second, "0.2.0"), hooks{
		afterLinkRemove: func(path string) error {
			if path == layout.legacyPluginLink {
				return injected
			}
			return nil
		},
	})

	// Then
	require.ErrorIs(t, err, injected)
	require.Equal(t, "releases/0.1.0-"+hostTarget(), currentTarget(t, layout.dataRoot))
	requireOwnedBinaryLink(t, layout)
	requireOwnedLegacyPluginLink(t, layout)
	require.NoDirExists(t, filepath.Join(layout.dataRoot, "releases", "0.2.0-"+hostTarget()))
}

func Test_Install_postswitch_sync_failure_restores_old_pointer(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	first := writeTestBundle(t, "0.1.0", "first")
	second := writeTestBundle(t, "0.2.0", "second")
	require.NoError(t, Install(context.Background(), layout.config(first, "0.1.0")))
	injected := errors.New("injected current sync failure")

	// When
	err := installWithHooks(context.Background(), layout.config(second, "0.2.0"), hooks{
		afterCurrentRename: func() error { return injected },
	})

	// Then
	require.ErrorIs(t, err, injected)
	require.Equal(t, "releases/0.1.0-"+hostTarget(), currentTarget(t, layout.dataRoot))
	require.NoDirExists(t, filepath.Join(layout.dataRoot, "releases", "0.2.0-"+hostTarget()))
}

func Test_Install_initial_postswitch_sync_failure_removes_visible_state(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	injected := errors.New("injected current sync failure")

	// When
	err := installWithHooks(context.Background(), layout.config(bundle, "0.1.0"), hooks{
		afterCurrentRename: func() error { return injected },
	})

	// Then
	require.ErrorIs(t, err, injected)
	require.NoFileExists(t, filepath.Join(layout.dataRoot, "current"))
	require.NoFileExists(t, layout.binLink)
	require.NoFileExists(t, layout.legacyPluginLink)
}

func Test_Install_verifies_prepared_release_binary_instead_of_bundle_source(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	marker := filepath.Join(t.TempDir(), "executed-path")
	bundle := writeTestBundleWithOptions(t, testBundleOptions{
		version: "0.1.0", marker: "prepared", binaryPathMarker: marker,
	})

	// When
	err := Install(context.Background(), layout.config(bundle, "0.1.0"))

	// Then
	require.NoError(t, err)
	executedPath, readErr := os.ReadFile(marker)
	require.NoError(t, readErr)
	require.Equal(t, filepath.Join(layout.dataRoot, "releases", "0.1.0-"+hostTarget(), "bin", "managed-bash"), string(executedPath))
}

func Test_Install_refuses_release_destination_created_during_publication(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")

	// When
	err := installWithHooks(context.Background(), layout.config(bundle, "0.1.0"), hooks{
		beforeReleaseRename: func(final string) {
			require.NoError(t, os.Mkdir(final, 0o700))
		},
	})

	// Then
	require.ErrorIs(t, err, ErrForeignPath)
	entries, readErr := os.ReadDir(filepath.Join(layout.dataRoot, "releases"))
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	require.Equal(t, "0.1.0-"+hostTarget(), entries[0].Name())
}

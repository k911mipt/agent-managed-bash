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

func Test_Install_refuses_foreign_registration(t *testing.T) {
	tests := []struct {
		name string
		path func(testLayout) string
		make func(*testing.T, string)
	}{
		{"binary regular file", func(layout testLayout) string { return layout.binLink }, writeForeignFile},
		{"plugin regular file", func(layout testLayout) string { return layout.pluginLink }, writeForeignFile},
		{"binary foreign symlink", func(layout testLayout) string { return layout.binLink }, writeForeignSymlink},
		{"plugin foreign symlink", func(layout testLayout) string { return layout.pluginLink }, writeForeignSymlink},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			layout := newTestLayout(t)
			bundle := writeTestBundle(t, "0.1.0", "first")
			path := test.path(layout)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			test.make(t, path)

			// When
			err := Install(context.Background(), layout.config(bundle, "0.1.0"))

			// Then
			require.ErrorIs(t, err, ErrForeignPath)
			require.NoFileExists(t, filepath.Join(layout.dataRoot, "current"))
		})
	}
}

func Test_Install_partial_initial_registration_is_rolled_back(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	injected := errors.New("injected plugin registration failure")

	// When
	err := installWithHooks(context.Background(), layout.config(bundle, "0.1.0"), hooks{
		beforePluginLink: func() error { return injected },
	})

	// Then
	require.ErrorIs(t, err, injected)
	require.NoFileExists(t, layout.binLink)
	require.NoFileExists(t, layout.pluginLink)
	require.NoFileExists(t, filepath.Join(layout.dataRoot, "current"))
	require.NoDirExists(t, filepath.Join(layout.dataRoot, "releases", "0.1.0-"+hostTarget()))
}

func Test_Install_cleanup_failure_retains_release_for_visible_registration(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	first := writeTestBundle(t, "0.1.0", "first")
	second := writeTestBundle(t, "0.2.0", "second")
	require.NoError(t, Install(context.Background(), layout.config(first, "0.1.0")))
	require.NoError(t, os.Remove(layout.binLink))
	publicationErr := errors.New("injected registration sync failure")
	cleanupErr := errors.New("injected registration cleanup failure")

	// When
	err := installWithHooks(context.Background(), layout.config(second, "0.2.0"), hooks{
		afterLinkRename: func(path string) error {
			if path == layout.binLink {
				return publicationErr
			}
			return nil
		},
		beforeLinkCleanup: func(path string) error {
			if path == layout.binLink {
				return cleanupErr
			}
			return nil
		},
	})

	// Then
	require.ErrorIs(t, err, publicationErr)
	require.ErrorIs(t, err, cleanupErr)
	require.Equal(t, "releases/0.1.0-"+hostTarget(), currentTarget(t, layout.dataRoot))
	requireOwnedLinks(t, layout)
	require.DirExists(t, filepath.Join(layout.dataRoot, "releases", "0.2.0-"+hostTarget()))
}

func Test_Install_postrename_registration_failure_is_rolled_back(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	injected := errors.New("injected registration sync failure")

	// When
	err := installWithHooks(context.Background(), layout.config(bundle, "0.1.0"), hooks{
		afterLinkRename: func(path string) error {
			if path == layout.binLink {
				return injected
			}
			return nil
		},
	})

	// Then
	require.ErrorIs(t, err, injected)
	require.NoFileExists(t, layout.binLink)
	require.NoFileExists(t, layout.pluginLink)
	require.NoFileExists(t, filepath.Join(layout.dataRoot, "current"))
}

func Test_Install_refuses_registration_inside_installation_data(t *testing.T) {
	tests := []struct {
		name      string
		configure func(testLayout)
	}{
		{"binary", func(layout testLayout) { layout.environment["MANAGED_BASH_BIN_DIR"] = layout.dataRoot }},
		{"plugin", func(layout testLayout) {
			layout.environment["XDG_CONFIG_HOME"] = filepath.Join(layout.dataRoot, "config")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			layout := newTestLayout(t)
			test.configure(layout)
			bundle := writeTestBundle(t, "0.1.0", "first")

			// When
			err := Install(context.Background(), layout.config(bundle, "0.1.0"))

			// Then
			require.ErrorIs(t, err, ErrUnsafePath)
			require.NoDirExists(t, layout.dataRoot)
		})
	}
}

func Test_Install_serializes_concurrent_operations(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	firstLocked := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondBeforeLock := make(chan struct{})
	secondLocked := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		results <- installWithHooks(context.Background(), layout.config(bundle, "0.1.0"), hooks{
			afterLock: func() { close(firstLocked); <-releaseFirst },
		})
	}()
	<-firstLocked
	go func() {
		results <- installWithHooks(context.Background(), layout.config(bundle, "0.1.0"), hooks{
			beforeLock: func() { close(secondBeforeLock) }, afterLock: func() { close(secondLocked) },
		})
	}()
	<-secondBeforeLock

	// When
	select {
	case <-secondLocked:
		t.Fatal("second installer acquired the held installation lock")
	default:
	}
	close(releaseFirst)

	// Then
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	<-secondLocked
}

func Test_Uninstall_is_idempotent_and_preserves_workspace_state(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	workspaceState := filepath.Join(physicalTempDir(t), "workspace", ".managed_bash", "jobs", "job-1", "state.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(workspaceState), 0o700))
	want := []byte("workspace-state-byte-for-byte")
	require.NoError(t, os.WriteFile(workspaceState, want, 0o600))
	require.NoError(t, Install(context.Background(), layout.config(bundle, "0.1.0")))

	// When
	firstErr := Uninstall(context.Background(), layout.config("", "0.1.0"))
	secondErr := Uninstall(context.Background(), layout.config("", "0.1.0"))
	got, readErr := os.ReadFile(workspaceState)

	// Then
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.NoError(t, readErr)
	require.Equal(t, want, got)
	require.NoFileExists(t, layout.binLink)
	require.NoFileExists(t, layout.pluginLink)
	require.NoDirExists(t, filepath.Join(layout.dataRoot, "releases"))
}

func Test_Uninstall_refuses_foreign_current_before_removing_registrations(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	require.NoError(t, Install(context.Background(), layout.config(bundle, "0.1.0")))
	require.NoError(t, os.Remove(filepath.Join(layout.dataRoot, "current")))
	require.NoError(t, os.Symlink("foreign", filepath.Join(layout.dataRoot, "current")))

	// When
	err := Uninstall(context.Background(), layout.config("", "0.1.0"))

	// Then
	require.ErrorIs(t, err, ErrForeignPath)
	requireOwnedLinks(t, layout)
}

func writeForeignFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("foreign"), 0o644))
}

func writeForeignSymlink(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "foreign"), path))
}

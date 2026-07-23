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
		name        string
		path        func(testLayout) string
		make        func(*testing.T, string) string
		wantSymlink bool
	}{
		{"binary regular file", func(layout testLayout) string { return layout.binLink }, writeForeignFile, false},
		{"plugin regular file", func(layout testLayout) string { return layout.legacyPluginLink }, writeForeignFile, false},
		{"binary foreign symlink", func(layout testLayout) string { return layout.binLink }, writeForeignSymlink, true},
		{"plugin foreign symlink", func(layout testLayout) string { return layout.legacyPluginLink }, writeForeignSymlink, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			layout := newTestLayout(t)
			bundle := writeTestBundle(t, "0.1.0", "first")
			path := test.path(layout)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			want := test.make(t, path)

			// When
			err := Install(context.Background(), layout.config(bundle, "0.1.0"))

			// Then
			require.ErrorIs(t, err, ErrForeignPath)
			require.NoFileExists(t, filepath.Join(layout.dataRoot, "current"))
			info, statErr := os.Lstat(path)
			require.NoError(t, statErr)
			isSymlink := info.Mode()&os.ModeSymlink != 0
			require.Equal(t, test.wantSymlink, isSymlink)
			if isSymlink {
				target, readErr := os.Readlink(path)
				require.NoError(t, readErr)
				require.Equal(t, want, target)
				return
			}
			contents, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.Equal(t, want, string(contents))
		})
	}
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
	requireOwnedBinaryLink(t, layout)
	require.NoFileExists(t, layout.legacyPluginLink)
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
	require.NoFileExists(t, layout.legacyPluginLink)
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
	require.NoFileExists(t, layout.legacyPluginLink)
	require.NoDirExists(t, filepath.Join(layout.dataRoot, "releases"))
}

func Test_Uninstall_removes_owned_legacy_plugin_registration(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	require.NoError(t, Install(context.Background(), layout.config(bundle, "0.1.0")))
	writeOwnedLegacyPluginLink(t, layout)

	// When
	err := Uninstall(context.Background(), layout.config("", "0.1.0"))

	// Then
	require.NoError(t, err)
	_, statErr := os.Lstat(layout.legacyPluginLink)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func Test_Uninstall_refuses_foreign_current_before_removing_registrations(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	require.NoError(t, Install(context.Background(), layout.config(bundle, "0.1.0")))
	writeOwnedLegacyPluginLink(t, layout)
	require.NoError(t, os.Remove(filepath.Join(layout.dataRoot, "current")))
	require.NoError(t, os.Symlink("foreign", filepath.Join(layout.dataRoot, "current")))

	// When
	err := Uninstall(context.Background(), layout.config("", "0.1.0"))

	// Then
	require.ErrorIs(t, err, ErrForeignPath)
	requireOwnedBinaryLink(t, layout)
	require.FileExists(t, layout.legacyPluginLink)
}

func writeForeignFile(t *testing.T, path string) string {
	t.Helper()
	const contents = "foreign"
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return contents
}

func writeForeignSymlink(t *testing.T, path string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "foreign")
	require.NoError(t, os.Symlink(target, path))
	return target
}

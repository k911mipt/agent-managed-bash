//go:build linux || darwin

package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type lockResult struct {
	lock *installationLock
	err  error
}

func Test_AcquireInstallationLock_rejects_final_symlink(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	paths, err := pathsFromConfig(layout.config("", "0.1.0"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(layout.environment["XDG_DATA_HOME"], ".agent-managed-bash.install.lock"), paths.lock)
	require.NoError(t, ensureDirectory(filepath.Dir(paths.lock), 0o700))
	target := filepath.Join(t.TempDir(), "target.lock")
	require.NoError(t, os.WriteFile(target, nil, 0o600))
	require.NoError(t, os.Symlink(target, paths.lock))

	// When
	lock, err := acquireInstallationLock(context.Background(), paths, hooks{})

	// Then
	if lock != nil {
		require.NoError(t, lock.close())
	}
	require.ErrorIs(t, err, ErrUnsafePath)
	require.FileExists(t, target)
}

func Test_AcquireInstallationLock_retries_stale_unlinked_waiter_inode(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	paths, err := pathsFromConfig(layout.config("", "0.1.0"))
	require.NoError(t, err)
	first, err := acquireInstallationLock(context.Background(), paths, hooks{})
	require.NoError(t, err)
	secondOpened := make(chan struct{})
	secondRetried := make(chan struct{})
	secondAcquired := make(chan struct{})
	results := make(chan lockResult, 1)
	go func() {
		lock, lockErr := acquireInstallationLock(context.Background(), paths, hooks{
			afterLockOpen: func(attempt int) {
				if attempt == 0 {
					close(secondOpened)
				} else if attempt == 1 {
					close(secondRetried)
				}
			},
			afterLock: func() { close(secondAcquired) },
		})
		results <- lockResult{lock: lock, err: lockErr}
	}()
	<-secondOpened
	require.NoError(t, os.Remove(paths.lock))
	third, err := acquireInstallationLock(context.Background(), paths, hooks{})
	require.NoError(t, err)

	// When
	require.NoError(t, first.close())
	<-secondRetried
	select {
	case <-secondAcquired:
		t.Fatal("stale waiter acquired a different lock inode concurrently")
	default:
	}
	require.NoError(t, third.close())

	// Then
	result := <-results
	require.NoError(t, result.err)
	require.NotNil(t, result.lock)
	<-secondAcquired
	require.NoError(t, result.lock.close())
}

func Test_AcquireInstallationLock_honors_context_cancellation(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	paths, err := pathsFromConfig(layout.config("", "0.1.0"))
	require.NoError(t, err)
	first, err := acquireInstallationLock(context.Background(), paths, hooks{})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	opened := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		lock, lockErr := acquireInstallationLock(ctx, paths, hooks{afterLockOpen: func(int) { close(opened) }})
		if lock != nil {
			lockErr = errors.Join(lockErr, lock.close())
		}
		result <- lockErr
	}()
	<-opened

	// When
	cancel()

	// Then
	require.ErrorIs(t, <-result, context.Canceled)
	require.NoError(t, first.close())
}

func Test_Uninstall_serializes_install_through_full_namespace_cleanup(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	require.NoError(t, Install(context.Background(), layout.config(bundle, "0.1.0")))
	cleanupReached := make(chan struct{})
	finishCleanup := make(chan struct{})
	uninstallResult := make(chan error, 1)
	go func() {
		uninstallResult <- uninstallWithHooks(context.Background(), layout.config("", "0.1.0"), hooks{
			beforeLockUnlink: func() { close(cleanupReached); <-finishCleanup },
		})
	}()
	<-cleanupReached
	require.NoDirExists(t, layout.dataRoot)
	installOpenedLock := make(chan struct{})
	installAcquiredLock := make(chan struct{})
	installResult := make(chan error, 1)
	var opened sync.Once
	go func() {
		installResult <- installWithHooks(context.Background(), layout.config(bundle, "0.1.0"), hooks{
			afterLockOpen: func(int) { opened.Do(func() { close(installOpenedLock) }) },
			afterLock:     func() { close(installAcquiredLock) },
		})
	}()
	<-installOpenedLock

	// When
	select {
	case <-installAcquiredLock:
		t.Fatal("install acquired the lock before uninstall finished namespace cleanup")
	default:
	}
	close(finishCleanup)

	// Then
	require.NoError(t, <-uninstallResult)
	require.NoError(t, <-installResult)
	<-installAcquiredLock
	requireOwnedLinks(t, layout)
}

func Test_Install_preserves_registration_created_by_racer(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	foreign := []byte("foreign-registration")

	// When
	err := installWithHooks(context.Background(), layout.config(bundle, "0.1.0"), hooks{
		beforeLinkRename: func(path string) {
			if path == layout.binLink {
				require.NoError(t, os.WriteFile(path, foreign, 0o644))
			}
		},
	})

	// Then
	require.ErrorIs(t, err, ErrForeignPath)
	content, readErr := os.ReadFile(layout.binLink)
	require.NoError(t, readErr)
	require.Equal(t, foreign, content)
}

func Test_Install_rejects_shared_writable_nonsticky_ancestor(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	shared := filepath.Join(t.TempDir(), "shared")
	require.NoError(t, os.Mkdir(shared, 0o700))
	require.NoError(t, os.Chmod(shared, 0o777))
	layout.environment["XDG_DATA_HOME"] = filepath.Join(shared, "data")

	// When
	err := Install(context.Background(), layout.config(bundle, "0.1.0"))

	// Then
	require.True(t, errors.Is(err, ErrUnsafePath))
	require.NoFileExists(t, layout.binLink)
}

//go:build linux || darwin

package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func Test_status_rejects_unsafe_terminal_intent_without_removing_it(t *testing.T) {
	tests := map[string]func(*testing.T, string){
		"symlink": func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "target")
			require.NoError(t, os.WriteFile(target, []byte("external"), privateFileMode))
			require.NoError(t, os.Symlink(target, path))
		},
		"directory": func(t *testing.T, path string) {
			require.NoError(t, os.Mkdir(path, privateDirectoryMode))
		},
		"fifo": func(t *testing.T, path string) {
			require.NoError(t, unix.Mkfifo(path, privateFileMode))
		},
		"wrong mode": func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, nil, 0o644))
			require.NoError(t, os.Chmod(path, 0o644))
		},
		"multiple links": func(t *testing.T, path string) {
			source := path + ".source"
			require.NoError(t, os.WriteFile(source, nil, privateFileMode))
			require.NoError(t, os.Link(source, path))
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			store, initial, lease := newInternalTestJob(t)
			path := filepath.Join(store.workspace, ".managed_bash", "jobs", string(initial.Job.JobID), terminalIntentName)
			setup(t, path)
			require.NoError(t, lease.release())

			_, err := store.status(context.Background(), initial.Job.JobID)

			require.ErrorIs(t, err, ErrUnsafeFilesystem)
			_, statErr := os.Lstat(path)
			require.NoError(t, statErr)
		})
	}
}

func Test_terminalIntentExists_rejects_unsafe_entry_before_waiting(t *testing.T) {
	store, initial, lease := newInternalTestJob(t)
	t.Cleanup(func() { require.NoError(t, lease.release()) })
	path := filepath.Join(store.workspace, ".managed_bash", "jobs", string(initial.Job.JobID), terminalIntentName)
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	err := store.waitForTerminalIntent(context.Background(), initial.Job.JobID, time.Now().Add(time.Second))

	require.ErrorIs(t, err, ErrUnsafeFilesystem)
}

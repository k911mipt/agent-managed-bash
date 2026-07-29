//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_removeTerminal_removes_empty_parent_when_contents_sync_fails(t *testing.T) {
	store, initial, lease := newInternalTestJob(t)
	exitCode := 0
	require.NoError(t, store.publishExecutionTerminal(initial.Job.JobID, executionOutcome{
		cause: causeNormal,
		wait:  shellWaitResult{exitCode: &exitCode},
	}, lease))
	syncFailure := errors.New("injected directory sync failure")
	syncCalls := 0
	store.syncDirectory = func(*os.File) error {
		syncCalls++
		return syncFailure
	}

	removeErr := store.removeTerminal(context.Background(), initial.Job.JobID)
	_, statErr := os.Stat(filepath.Join(store.workspace, ".managed_bash", "jobs", string(initial.Job.JobID)))

	require.ErrorIs(t, removeErr, syncFailure)
	require.Equal(t, 1, syncCalls)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func Test_removeDirectoryContents_rejects_nested_directory_before_unlinking(t *testing.T) {
	path := t.TempDir()
	nestedPath := filepath.Join(path, "nested")
	require.NoError(t, os.Mkdir(nestedPath, privateDirectoryMode))
	directory, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, directory.Close()) })

	result, err := removeDirectoryContents(directory, func(file *os.File) error { return file.Sync() })

	require.ErrorIs(t, err, ErrUnsafeFilesystem)
	require.False(t, result.emptied)
	info, statErr := os.Stat(nestedPath)
	require.NoError(t, statErr)
	require.True(t, info.IsDir())
}

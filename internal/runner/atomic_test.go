//go:build linux || darwin

package runner_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/stretchr/testify/require"
)

func Test_Store_publish_state_never_exposes_partial_json(t *testing.T) {
	// Given
	store, workspace := newTestStore(t)
	initial := runningState(workspace, 10)
	lease := commitTestJob(t, store, initial)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	statePath := filepath.Join(jobPath(workspace, initial.Job.JobID), "state.json")
	started := make(chan struct{})
	done := make(chan struct{})
	readerResult := make(chan error, 1)
	go readStateUntilDone(statePath, started, done, readerResult)
	<-started

	// When
	for index := range 100 {
		next := initial
		next.Observers = []generated.ObserverCursor{{
			SessionID: "observer", UpdatedAtUnixMs: generated.TimestampUnixMs(103 + index),
		}}
		require.NoError(t, store.PublishState(initial.Job.JobID, next))
	}
	close(done)

	// Then
	require.NoError(t, <-readerResult)
}

func readStateUntilDone(path string, started chan<- struct{}, done <-chan struct{}, result chan<- error) {
	firstRead := true
	for {
		raw, err := os.ReadFile(path)
		if err != nil {
			result <- fmt.Errorf("read state: %w", err)
			return
		}
		var persisted generated.PersistedJobState
		if err := json.Unmarshal(raw, &persisted); err != nil {
			result <- fmt.Errorf("decode state: %w", err)
			return
		}
		if firstRead {
			close(started)
			firstRead = false
		}
		select {
		case <-done:
			result <- nil
			return
		default:
		}
	}
}

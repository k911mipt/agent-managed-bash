//go:build linux || darwin

package runner

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/stretchr/testify/require"
)

const (
	waitTestIdleTimeout = 10 * time.Millisecond
	waitTestTimeout     = 100 * time.Millisecond
)

type waitTestFixture struct {
	manager    *Manager
	invocation state.TrustedInvocation
	jobID      generated.JobID
	store      *Store
}

type preparedWaitResult struct {
	prepared *PreparedWait
	err      error
}

func Test_Manager_prepare_wait_returns_all_output_when_each_append_precedes_idle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given
		fixture := newWaitTestFixture(t)
		fixture.append(t, "a")
		result := fixture.prepare()
		synctest.Wait()

		// When
		time.Sleep(waitTestIdleTimeout - time.Millisecond)
		synctest.Wait()
		fixture.append(t, "b")
		time.Sleep(waitTestIdleTimeout - time.Millisecond)
		synctest.Wait()
		fixture.append(t, "c")
		synctest.Wait()
		time.Sleep(waitTestIdleTimeout)
		prepared := receivePreparedWait(t, result)

		// Then
		require.Equal(t, "abc", prepared.Observation.Output.Text)
		require.Equal(t, generated.ByteCursor(3), prepared.Observation.Output.NextCursorBytes)
		require.Equal(t, generated.JobStatusRunning, prepared.Observation.Observation.Job.Status)
	})
}

func Test_Manager_prepare_wait_returns_checkpoint_before_output_arrives_after_idle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given
		fixture := newWaitTestFixture(t)
		result := fixture.prepare()
		synctest.Wait()
		fixture.append(t, "a")
		time.Sleep(time.Millisecond)
		synctest.Wait()

		// When
		time.Sleep(waitTestIdleTimeout)

		// Then
		checkpoint := receivePreparedWait(t, result)
		require.Equal(t, "a", checkpoint.Observation.Output.Text)
		require.Equal(t, generated.ByteCursor(1), checkpoint.Observation.Output.NextCursorBytes)
		require.Equal(t, generated.JobStatusRunning, checkpoint.Observation.Observation.Job.Status)
		require.NoError(t, checkpoint.commit(context.Background()))
		fixture.append(t, "b")
		nextResult := fixture.prepare()
		synctest.Wait()
		time.Sleep(waitTestIdleTimeout)
		next := receivePreparedWait(t, nextResult)
		require.Equal(t, "b", next.Observation.Output.Text)
		require.Equal(t, generated.ByteCursor(2), next.Observation.Output.NextCursorBytes)
	})
}

func Test_Manager_prepare_wait_restarts_idle_when_final_read_observes_new_output(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given
		fixture := newWaitTestFixture(t)
		hookResult := make(chan error, 1)
		fixture.manager.beforeWaitOutput = func() {
			fixture.manager.beforeWaitOutput = nil
			_, err := fixture.store.appendOutput(fixture.jobID, []byte("b"))
			hookResult <- err
		}
		result := fixture.prepare()
		synctest.Wait()
		fixture.append(t, "a")
		time.Sleep(time.Millisecond)
		synctest.Wait()

		// When
		time.Sleep(waitTestIdleTimeout)
		synctest.Wait()
		require.NoError(t, <-hookResult)

		// Then
		select {
		case early := <-result:
			require.Failf(t, "wait returned before new quiet interval", "output=%q error=%v", early.prepared.Observation.Output.Text, early.err)
		default:
		}
		time.Sleep(waitTestIdleTimeout)
		prepared := receivePreparedWait(t, result)
		require.Equal(t, "ab", prepared.Observation.Output.Text)
		require.Equal(t, generated.ByteCursor(2), prepared.Observation.Output.NextCursorBytes)
		require.Equal(t, generated.JobStatusRunning, prepared.Observation.Observation.Job.Status)
	})
}

func Test_Manager_prepare_wait_preserves_absolute_timeout_when_final_read_observes_new_output(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given
		fixture := newWaitTestFixture(t)
		hookResult := make(chan error, 1)
		fixture.manager.beforeWaitOutput = func() {
			fixture.manager.beforeWaitOutput = nil
			_, err := fixture.store.appendOutput(fixture.jobID, []byte("b"))
			hookResult <- err
		}
		result := fixture.prepareWithTimeout(waitTestIdleTimeout)
		synctest.Wait()
		fixture.append(t, "a")
		time.Sleep(time.Millisecond)
		synctest.Wait()

		// When
		time.Sleep(waitTestIdleTimeout - time.Millisecond)

		// Then
		require.NoError(t, <-hookResult)
		prepared := receivePreparedWait(t, result)
		require.Equal(t, "ab", prepared.Observation.Output.Text)
		require.Equal(t, generated.JobStatusRunning, prepared.Observation.Observation.Job.Status)
	})
}

func newWaitTestFixture(t *testing.T) waitTestFixture {
	t.Helper()
	manager, invocation, jobID, _ := newStateLockFixture(t)
	manager.config.PollInterval = time.Millisecond
	contracts, err := contract.Load()
	require.NoError(t, err)
	store, err := OpenStore(invocation, contracts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return waitTestFixture{manager: manager, invocation: invocation, jobID: jobID, store: store}
}

func (fixture waitTestFixture) append(t *testing.T, output string) {
	t.Helper()
	appendResult, err := fixture.store.appendOutput(fixture.jobID, []byte(output))
	require.NoError(t, err)
	require.Equal(t, len(output), appendResult.AcceptedBytes)
}

func (fixture waitTestFixture) prepare() <-chan preparedWaitResult {
	return fixture.prepareWithTimeout(waitTestTimeout)
}

func (fixture waitTestFixture) prepareWithTimeout(timeout time.Duration) <-chan preparedWaitResult {
	result := make(chan preparedWaitResult, 1)
	go func() {
		prepared, err := fixture.manager.PrepareWait(context.Background(), WaitRequest{
			Invocation: fixture.invocation, JobID: fixture.jobID,
			Timeout: timeout, IdleTimeout: waitTestIdleTimeout,
		})
		result <- preparedWaitResult{prepared: prepared, err: err}
	}()
	return result
}

func receivePreparedWait(t *testing.T, result <-chan preparedWaitResult) *PreparedWait {
	t.Helper()
	prepared := <-result
	require.NoError(t, prepared.err)
	require.NotNil(t, prepared.prepared)
	return prepared.prepared
}

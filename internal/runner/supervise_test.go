//go:build linux || darwin

package runner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type countingCanceledContext struct {
	done     <-chan struct{}
	errCalls atomic.Int32
}

func newCountingCanceledContext() *countingCanceledContext {
	done := make(chan struct{})
	close(done)
	return &countingCanceledContext{done: done}
}

func (ctx *countingCanceledContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *countingCanceledContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *countingCanceledContext) Err() error {
	ctx.errCalls.Add(1)
	return context.Canceled
}

func (*countingCanceledContext) Value(any) any {
	return nil
}

func Test_superviseShell_handles_context_cancellation_once(t *testing.T) {
	// Given
	cwd, err := openWorkspaceDirectory(testWorkspace(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cwd.Close()) })
	shell, _, err := startShell(internalStartRequest{
		Cwd: testWorkspace(t), Command: `trap '' TERM; exec sleep 10`, HardTimeoutMs: 10_000,
	}, cwd)
	require.NoError(t, err)
	ctx := newCountingCanceledContext()
	started := time.Now()

	// When
	_, err = superviseShell(ctx, nil, "", shell, 20*time.Millisecond, time.Second)

	// Then
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 1, ctx.errCalls.Load())
	require.Less(t, time.Since(started), time.Second)
}

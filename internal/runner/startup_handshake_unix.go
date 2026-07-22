//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

type startupHandshake struct {
	manager   *Manager
	ctx       context.Context
	request   StartRequest
	jobID     generated.JobID
	command   *exec.Cmd
	waited    <-chan error
	input     *os.File
	response  *os.File
	workspace *os.File
	timeout   <-chan time.Time
}

func (startup startupHandshake) complete() (generated.JobMetadata, error) {
	defer startup.response.Close()
	prepared, err := startup.awaitFrame()
	if err != nil {
		return generated.JobMetadata{}, startup.abortBeforeCommit(err)
	}
	if prepared.kind == frameFailure {
		return generated.JobMetadata{}, startup.abortBeforeCommit(decodeStartupFailure(prepared))
	}
	if err := decodeFrame(prepared, framePrepared, &struct{}{}); err != nil {
		return generated.JobMetadata{}, startup.abortBeforeCommit(err)
	}
	if startup.manager.beforeCommit != nil {
		startup.manager.beforeCommit()
	}
	select {
	case <-startup.ctx.Done():
		return generated.JobMetadata{}, startup.abortBeforeCommit(startup.ctx.Err())
	case <-startup.timeout:
		return generated.JobMetadata{}, startup.abortBeforeCommit(ErrStartupTimeout)
	default:
	}
	commitTimer := time.NewTimer(startup.manager.config.StartupTimeout)
	defer commitTimer.Stop()
	if err := writeFrameBefore(startup.ctx, startup.input, frameCommit, struct{}{}, commitTimer.C); err != nil {
		return startup.recoverCommitted(errors.Join(err, startup.input.Close()))
	}
	if startup.manager.afterCommit != nil {
		startup.manager.afterCommit()
	}
	if err := startup.input.Close(); err != nil {
		return startup.recoverCommitted(fmt.Errorf("close committed bootstrap input: %w", err))
	}
	committed, err := startup.awaitCommittedFrame()
	if err != nil {
		return startup.recoverCommitted(err)
	}
	if committed.kind == frameFailure {
		return startup.recoverCommitted(decodeStartupFailure(committed))
	}
	var result internalCommitted
	if err := decodeFrame(committed, frameCommitted, &result); err != nil {
		return startup.recoverCommitted(err)
	}
	return committedResult(result)
}

func committedResult(result internalCommitted) (generated.JobMetadata, error) {
	if result.DurabilityError == "" {
		return result.Job, nil
	}
	return result.Job, &CommitDurabilityError{JobID: result.Job.JobID, Cause: errors.New(result.DurabilityError)}
}

func (startup startupHandshake) awaitCommittedFrame() (internalFrame, error) {
	frames := make(chan frameResult, 1)
	go func() {
		frame, err := readFrame(startup.response)
		frames <- frameResult{frame: frame, err: err}
	}()
	timer := time.NewTimer(startup.manager.config.StartupTimeout)
	defer timer.Stop()
	select {
	case result := <-frames:
		return result.frame, result.err
	case <-timer.C:
		return internalFrame{}, ErrStartupTimeout
	}
}

func (startup startupHandshake) recoverCommitted(cause error) (job generated.JobMetadata, returnErr error) {
	store, err := OpenStoreAt(startup.request.Invocation, startup.manager.contracts, startup.workspace)
	if err != nil {
		return generated.JobMetadata{}, errors.Join(cause, err)
	}
	defer func() { returnErr = errors.Join(returnErr, store.Close()) }()
	deadline := time.Now().Add(startup.manager.config.StartupTimeout)
	recoveryContext, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	var loadErr error
	for {
		if !time.Now().Before(deadline) {
			return generated.JobMetadata{}, errors.Join(cause, loadErr, context.DeadlineExceeded)
		}
		snapshot, err := store.loadContext(recoveryContext, startup.jobID)
		if err == nil {
			return snapshot.State.Job, &CommitDurabilityError{
				JobID: snapshot.State.Job.JobID, Cause: errors.Join(ErrCommitDurabilityUnknown, cause),
			}
		}
		loadErr = err
		if recoveryContext.Err() != nil || !time.Now().Before(deadline) {
			return generated.JobMetadata{}, errors.Join(cause, loadErr, context.DeadlineExceeded)
		}
		pause := min(startup.manager.config.PollInterval, time.Until(deadline))
		timer := time.NewTimer(pause)
		select {
		case <-recoveryContext.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return generated.JobMetadata{}, errors.Join(cause, loadErr, recoveryContext.Err())
		case <-timer.C:
		}
	}
}

func (startup startupHandshake) awaitFrame() (internalFrame, error) {
	frames := make(chan frameResult, 1)
	go func() {
		frame, err := readFrame(startup.response)
		frames <- frameResult{frame: frame, err: err}
	}()
	select {
	case result := <-frames:
		return result.frame, result.err
	case <-startup.timeout:
		return internalFrame{}, ErrStartupTimeout
	case <-startup.ctx.Done():
		return internalFrame{}, startup.ctx.Err()
	}
}

func (startup startupHandshake) abortBeforeCommit(cause error) error {
	closeErr := startup.input.Close()
	timer := time.NewTimer(startup.manager.config.TerminationGrace + 100*time.Millisecond)
	defer timer.Stop()
	select {
	case waitErr := <-startup.waited:
		return errors.Join(cause, closeErr, waitErr)
	case <-timer.C:
		return startup.manager.stopBootstrapWithWait(startup.command, startup.waited, errors.Join(cause, closeErr))
	}
}

func decodeStartupFailure(frame internalFrame) error {
	var failure internalFailure
	if err := decodeFrame(frame, frameFailure, &failure); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrStartupFailed, failure.Message)
}

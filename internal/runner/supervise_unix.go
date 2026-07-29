//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"golang.org/x/sys/unix"
)

type executionCause uint8

const (
	causeNormal executionCause = iota
	causeOutputLimit
	causeHardTimeout
	causeCancelled
)

type executionOutcome struct {
	cause executionCause
	wait  shellWaitResult
}

func superviseShell(
	ctx context.Context,
	store *Store,
	jobID generated.JobID,
	shell *runningShell,
	grace time.Duration,
	pollInterval time.Duration,
) (executionOutcome, error) {
	defer shell.hardTimer.Stop()
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	pollEvents := poll.C
	var cause executionCause
	var waitResult shellWaitResult
	var operationErr error
	var processGroupSignalErrors []error
	waited := false
	captureDone := false
	cleanupStarted := false
	cleanupDone := false
	hardTimeout := shell.hardTimer.C
	canceled := ctx.Done()
	var graceTimer *time.Timer
	var graceExpired <-chan time.Time
	startCleanup := func() {
		if cleanupStarted {
			return
		}
		cleanupStarted = true
		processGroupSignalErrors = append(processGroupSignalErrors, shell.signalGroup(unix.SIGTERM))
		cleanupGrace := grace
		if cause == causeNormal {
			cleanupGrace = 0
		}
		graceTimer = time.NewTimer(cleanupGrace)
		graceExpired = graceTimer.C
	}
	for !waited || !captureDone || !cleanupDone {
		select {
		case event := <-shell.captured:
			if event.done {
				captureDone = true
				operationErr = errors.Join(operationErr, event.err)
				continue
			}
			if cause == causeOutputLimit || operationErr != nil {
				continue
			}
			appendResult, err := store.appendOutput(jobID, event.data)
			if err != nil {
				operationErr = errors.Join(operationErr, err)
				startCleanup()
				continue
			}
			if appendResult.LimitReached && cause == causeNormal {
				cause = causeOutputLimit
				hardTimeout = nil
				shell.hardTimer.Stop()
				startCleanup()
			}
		case reportedWait := <-shell.waited:
			if reportedWait.signal == nil {
				reportedWait.signal = waitResult.signal
			}
			waitResult = reportedWait
			waited = true
			if waitResult.err != nil && cause == causeNormal {
				operationErr = errors.Join(operationErr, waitResult.err)
			}
			hardTimeout = nil
			shell.hardTimer.Stop()
			startCleanup()
		case <-hardTimeout:
			hardTimeout = nil
			if cause == causeNormal {
				cause = causeHardTimeout
				startCleanup()
			}
		case <-graceExpired:
			graceExpired = nil
			processGroupSignalErrors = append(processGroupSignalErrors, shell.signalGroup(unix.SIGKILL))
			if waitResult.signal == nil && cause != causeNormal {
				signal := int(unix.SIGKILL)
				waitResult.signal = &signal
			}
			operationErr = errors.Join(operationErr, reapGuardian(shell.command))
			cleanupDone = true
		case <-canceled:
			canceled = nil
			operationErr = errors.Join(operationErr, ctx.Err())
			startCleanup()
		case <-pollEvents:
			snapshot, err := store.Load(jobID)
			if err != nil {
				operationErr = errors.Join(operationErr, err)
				startCleanup()
				continue
			}
			if snapshot.State.Cancellation != nil {
				pollEvents = nil
				cause = causeCancelled
				hardTimeout = nil
				shell.hardTimer.Stop()
				startCleanup()
			}
		}
	}
	if graceTimer != nil {
		graceTimer.Stop()
	}
	operationErr = errors.Join(operationErr, reconcileProcessGroupSignalErrors(shell.processGroup, processGroupSignalErrors...))
	if operationErr != nil {
		return executionOutcome{}, fmt.Errorf("supervise shell: %w", operationErr)
	}
	return executionOutcome{cause: cause, wait: waitResult}, nil
}

func stopUnpublishedShell(shell *runningShell, grace time.Duration) error {
	shell.hardTimer.Stop()
	signalErr := shell.signalGroup(unix.SIGTERM)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	waited := false
	captureDone := false
	killed := false
	var captureErr error
	for !waited || !captureDone {
		select {
		case <-shell.waited:
			waited = true
		case event := <-shell.captured:
			if event.done {
				captureDone = true
				captureErr = event.err
			}
		case <-timer.C:
			if !killed {
				signalErr = errors.Join(signalErr, shell.signalGroup(unix.SIGKILL))
				signalErr = errors.Join(signalErr, reapGuardian(shell.command))
				killed = true
			}
		}
	}
	if !killed {
		signalErr = errors.Join(signalErr, shell.signalGroup(unix.SIGKILL))
		signalErr = errors.Join(signalErr, reapGuardian(shell.command))
	}
	return errors.Join(signalErr, captureErr)
}

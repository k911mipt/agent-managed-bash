//go:build linux || darwin

package runner

import (
	"fmt"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

func terminalState(
	current generated.PersistedJobState,
	outcome executionOutcome,
) (generated.PersistedJobState, error) {
	finished := generated.TimestampUnixMs(time.Now().UnixMilli())
	finished = max(finished, current.Job.StartedAtUnixMs)
	result, status, err := processResult(outcome, finished, current.Job.CapturedBytes)
	if err != nil {
		return generated.PersistedJobState{}, err
	}
	current.Job.Status = status
	current.Job.FinishedAtUnixMs = &finished
	current.Result = &result
	return current, nil
}

func processResult(
	outcome executionOutcome,
	finished generated.TimestampUnixMs,
	captured generated.ByteCursor,
) (generated.ProcessResult, generated.JobStatus, error) {
	result := generated.ProcessResult{FinishedAtUnixMs: finished, CapturedBytes: captured}
	if outcome.cause == causeOutputLimit {
		result.Status = generated.TerminalStatusOutputLimit
		result.Signal = outcome.wait.signal
		return result, generated.JobStatusOutputLimit, nil
	}
	if outcome.cause == causeHardTimeout {
		result.Status = generated.TerminalStatusHardTimeout
		result.Signal = outcome.wait.signal
		return result, generated.JobStatusHardTimeout, nil
	}
	if outcome.cause == causeCancelled {
		result.Status = generated.TerminalStatusCancelled
		result.Signal = outcome.wait.signal
		return result, generated.JobStatusCancelled, nil
	}
	if outcome.wait.signal != nil {
		result.Status = generated.TerminalStatusSignalExit
		result.Signal = outcome.wait.signal
		return result, generated.JobStatusSignalExit, nil
	}
	if outcome.wait.exitCode != nil {
		exitCode := *outcome.wait.exitCode
		result.ExitCode = outcome.wait.exitCode
		if exitCode == 0 {
			result.Status = generated.TerminalStatusSucceeded
			return result, generated.JobStatusSucceeded, nil
		}
		if exitCode > 0 && exitCode <= 255 {
			result.Status = generated.TerminalStatusNonzeroExit
			return result, generated.JobStatusNonzeroExit, nil
		}
	}
	return generated.ProcessResult{}, "", fmt.Errorf("map process result: %w", ErrExecution)
}

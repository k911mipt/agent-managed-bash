//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"golang.org/x/sys/unix"
)

type runningShell struct {
	command       *exec.Cmd
	processGroup  int
	birthIdentity string
	shellPID      int
	waited        <-chan shellWaitResult
	captured      <-chan captureEvent
	hardTimer     *time.Timer
	lifetime      *os.File
}

type shellWaitResult struct {
	exitCode *int
	signal   *int
	err      error
}

func runDetachedRunner(ctx context.Context, input *os.File, bootstrapControl *os.File, workspace *os.File, cwd *os.File) (returnErr error) {
	frame, err := readFrame(input)
	if err != nil {
		return err
	}
	var request internalStartRequest
	if err := decodeFrame(frame, frameStart, &request); err != nil {
		return err
	}
	if err := validateInternalStart(request); err != nil {
		return err
	}
	contracts, err := contract.Load()
	if err != nil {
		return fmt.Errorf("load contracts: %w", err)
	}
	invocation, decision := contracts.Policy().BindCapabilityInvocation(
		state.HostInvocation{SessionID: request.SessionID, WorkspacePath: request.WorkspacePath, Cwd: request.Cwd},
		generated.TrustedContext{SessionID: request.SessionID, WorkspacePath: request.WorkspacePath, Cwd: request.Cwd},
	)
	if !decision.Allowed {
		return ErrInvalidStart
	}
	store, err := OpenStoreAt(invocation, contracts, workspace)
	if err != nil {
		return err
	}
	unix.CloseOnExec(fileFD(workspace))
	unix.CloseOnExec(fileFD(cwd))
	defer func() { returnErr = errors.Join(returnErr, store.Close()) }()
	shell, startedUnixMs, err := startShell(request, cwd)
	if err != nil {
		return err
	}
	defer shell.lifetime.Close()
	initial := initialJobState(request, startedUnixMs)
	runtime := RuntimeMetadata{
		RunnerPID: os.Getpid(), ShellPID: shell.shellPID, ProcessGroupID: shell.processGroup,
		ProcessGroupLeaderPID: shell.processGroup,
		ProcessBirthIdentity:  shell.birthIdentity,
	}
	pending, err := store.prepare(initial, runtime)
	if err != nil {
		return errors.Join(err, stopUnpublishedShell(shell, time.Duration(request.TerminationGraceMs)*time.Millisecond))
	}
	prepared := false
	defer func() {
		if !prepared {
			returnErr = errors.Join(returnErr, pending.abort())
		}
	}()
	if err := writeFrame(bootstrapControl, framePrepared, struct{}{}); err != nil {
		return errors.Join(err, stopUnpublishedShell(shell, time.Duration(request.TerminationGraceMs)*time.Millisecond))
	}
	commit, err := readFrame(input)
	if err != nil || commit.kind != frameCommit {
		return errors.Join(err, ErrStartupFailed, stopUnpublishedShell(shell, time.Duration(request.TerminationGraceMs)*time.Millisecond))
	}
	lease, commitErr := pending.commit()
	if lease == nil {
		return errors.Join(commitErr, stopUnpublishedShell(shell, time.Duration(request.TerminationGraceMs)*time.Millisecond))
	}
	prepared = true
	defer func() { returnErr = errors.Join(returnErr, lease.release()) }()
	committed := internalCommitted{Job: initial.Job}
	if commitErr != nil {
		committed.DurabilityError = commitErr.Error()
	}
	writeErr := writeFrame(bootstrapControl, frameCommitted, committed)
	closeErr := bootstrapControl.Close()
	if writeErr != nil {
		writeErr = errors.Join(writeErr, closeErr)
	}
	outcome, err := superviseShell(
		ctx, store, request.JobID, shell,
		time.Duration(request.TerminationGraceMs)*time.Millisecond,
		time.Duration(request.PollIntervalMs)*time.Millisecond,
	)
	if err != nil {
		return errors.Join(writeErr, err)
	}
	if err := store.publishExecutionTerminal(request.JobID, outcome, lease); err != nil {
		return errors.Join(writeErr, err)
	}
	return errors.Join(writeErr, commitErr)
}

func initialJobState(request internalStartRequest, started generated.TimestampUnixMs) generated.PersistedJobState {
	if started < request.CreatedAtUnixMs {
		started = request.CreatedAtUnixMs
	}
	return generated.PersistedJobState{
		SchemaVersion: 1,
		Session: generated.SessionMetadata{
			SchemaVersion: 1, SessionID: request.SessionID, WorkspacePath: request.WorkspacePath,
			CreatedAtUnixMs: request.CreatedAtUnixMs,
		},
		Job: generated.JobMetadata{
			JobID: request.JobID, Status: generated.JobStatusRunning, OwnerSessionID: request.SessionID,
			WorkspacePath: request.WorkspacePath, Cwd: request.Cwd, Command: request.Command,
			CreatedAtUnixMs: request.CreatedAtUnixMs, StartedAtUnixMs: started,
			HardTimeoutMs: request.HardTimeoutMs, OutputLimitBytes: request.OutputLimitBytes,
		},
		Observers: []generated.ObserverCursor{},
	}
}

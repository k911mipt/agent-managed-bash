//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const internalRunnerArgument = "--managed-bash-internal=runner"
const internalGuardianArgument = "--managed-bash-internal=guardian"

func DispatchInternal(ctx context.Context, args []string) (bool, error) {
	if len(args) != 1 {
		return false, nil
	}
	if args[0] == internalGuardianArgument {
		command := os.NewFile(3, "managed-bash-command")
		control := os.NewFile(4, "managed-bash-guardian-control")
		cwd := os.NewFile(5, "managed-bash-guardian-cwd")
		lifetime := os.NewFile(6, "managed-bash-guardian-lifetime")
		if command == nil || control == nil || cwd == nil || lifetime == nil {
			return true, ErrInternalProtocol
		}
		defer command.Close()
		defer control.Close()
		defer cwd.Close()
		defer lifetime.Close()
		return true, runGuardian(ctx, command, control, cwd, lifetime)
	}
	var handler func(context.Context, *os.File, *os.File, *os.File, *os.File) error
	switch args[0] {
	case internalBootstrapArgument:
		handler = runBootstrap
	case internalRunnerArgument:
		handler = runDetachedRunner
	default:
		return false, nil
	}
	control := os.NewFile(3, "managed-bash-control")
	workspace := os.NewFile(4, "managed-bash-workspace")
	cwd := os.NewFile(5, "managed-bash-cwd")
	if control == nil || workspace == nil || cwd == nil {
		return true, ErrInternalProtocol
	}
	defer control.Close()
	defer workspace.Close()
	defer cwd.Close()
	return true, handler(ctx, os.Stdin, control, workspace, cwd)
}

func runBootstrap(ctx context.Context, input *os.File, managerControl *os.File, workspace *os.File, cwd *os.File) error {
	frame, err := readFrame(input)
	if err != nil {
		return sendBootstrapFailure(managerControl, err)
	}
	var request internalStartRequest
	if err := decodeFrame(frame, frameStart, &request); err != nil {
		return sendBootstrapFailure(managerControl, err)
	}
	if err := validateInternalStart(request); err != nil {
		return sendBootstrapFailure(managerControl, err)
	}
	runnerInput, runnerWriter, err := os.Pipe()
	if err != nil {
		return sendBootstrapFailure(managerControl, fmt.Errorf("create runner input: %w", err))
	}
	runnerEvents, runnerControl, err := os.Pipe()
	if err != nil {
		return sendBootstrapFailure(managerControl, errors.Join(err, runnerInput.Close(), runnerWriter.Close()))
	}
	executable, err := os.Executable()
	if err != nil {
		return sendBootstrapFailure(managerControl, errors.Join(err, runnerInput.Close(), runnerWriter.Close(), runnerEvents.Close(), runnerControl.Close()))
	}
	command := exec.Command(executable, internalRunnerArgument)
	command.Stdin = runnerInput
	command.ExtraFiles = []*os.File{runnerControl, workspace, cwd}
	command.SysProcAttr = newSessionAttributes()
	if err := command.Start(); err != nil {
		return sendBootstrapFailure(managerControl, errors.Join(
			fmt.Errorf("start detached runner: %w", err), runnerInput.Close(), runnerWriter.Close(),
			runnerEvents.Close(), runnerControl.Close(),
		))
	}
	closeErr := errors.Join(runnerInput.Close(), runnerControl.Close())
	if closeErr != nil {
		return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, fmt.Errorf("close runner child descriptors: %w", closeErr), nil, request))
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	deadline := time.Now().Add(time.Duration(request.StartupTimeoutMs) * time.Millisecond)
	if err := writeFrame(runnerWriter, frameStart, request); err != nil {
		return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, err, waited, request))
	}
	prepared, err := awaitRunnerFrame(ctx, runnerEvents, deadline)
	if err != nil || prepared.kind != framePrepared {
		return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, errors.Join(err, ErrStartupFailed), waited, request))
	}
	if err := writeFrame(managerControl, framePrepared, struct{}{}); err != nil {
		return stopRunner(command, runnerWriter, err, waited, request)
	}
	commit, err := awaitRunnerFrame(ctx, input, deadline)
	if err != nil {
		return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, err, waited, request))
	}
	if err := decodeFrame(commit, frameCommit, &struct{}{}); err != nil {
		return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, err, waited, request))
	}
	if err := writeFrame(runnerWriter, frameCommit, struct{}{}); err != nil {
		return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, err, waited, request))
	}
	committed, err := awaitRunnerFrame(ctx, runnerEvents, deadline)
	if err != nil {
		return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, err, waited, request))
	}
	if committed.kind == frameFailure {
		var failure internalFailure
		if decodeErr := decodeFrame(committed, frameFailure, &failure); decodeErr != nil {
			return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, decodeErr, waited, request))
		}
		return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, fmt.Errorf("%w: %s", ErrStartupFailed, failure.Message), waited, request))
	}
	var result internalCommitted
	if err := decodeFrame(committed, frameCommitted, &result); err != nil {
		return sendBootstrapFailure(managerControl, stopRunner(command, runnerWriter, err, waited, request))
	}
	closeErr = errors.Join(runnerWriter.Close(), runnerEvents.Close())
	if err := writeFrame(managerControl, frameCommitted, result); err != nil {
		return errors.Join(err, closeErr)
	}
	return closeErr
}

func awaitRunnerFrame(
	ctx context.Context,
	reader *os.File,
	deadline time.Time,
) (internalFrame, error) {
	frames := make(chan frameResult, 1)
	go func() {
		frame, err := readFrame(reader)
		frames <- frameResult{frame: frame, err: err}
	}()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case result := <-frames:
		return result.frame, result.err
	case <-timer.C:
		return internalFrame{}, ErrStartupTimeout
	case <-ctx.Done():
		return internalFrame{}, ctx.Err()
	}
}

func stopRunner(
	command *exec.Cmd,
	input *os.File,
	cause error,
	waited <-chan error,
	request internalStartRequest,
) error {
	closeErr := input.Close()
	if waited == nil {
		waitChannel := make(chan error, 1)
		go func() { waitChannel <- command.Wait() }()
		waited = waitChannel
	}
	timer := time.NewTimer(time.Duration(request.TerminationGraceMs)*time.Millisecond + time.Second)
	defer timer.Stop()
	select {
	case waitErr := <-waited:
		return errors.Join(cause, closeErr, waitErr)
	case <-timer.C:
		killErr := command.Process.Kill()
		return errors.Join(cause, closeErr, killErr, <-waited)
	}
}

func sendBootstrapFailure(control *os.File, cause error) error {
	writeErr := writeFrame(control, frameFailure, internalFailure{Message: cause.Error()})
	return errors.Join(cause, writeErr)
}

func validateInternalStart(request internalStartRequest) error {
	if !validJobID(request.JobID) || request.SessionID == "" || request.WorkspacePath == "" ||
		request.Cwd == "" || request.Command == "" || request.HardTimeoutMs < 1 ||
		request.OutputLimitBytes < 1 || request.StartupTimeoutMs < 1 || request.TerminationGraceMs < 1 ||
		request.PollIntervalMs < 1 {
		return ErrInvalidStart
	}
	return nil
}

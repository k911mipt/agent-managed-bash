//go:build linux || darwin

package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"golang.org/x/sys/unix"
)

const guardianShellScript = `eval -- "$(cat <&3)"`

func startShell(request internalStartRequest, cwd *os.File) (*runningShell, generated.TimestampUnixMs, error) {
	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		return nil, 0, fmt.Errorf("create merged output pipe: %w", err)
	}
	commandReader, commandWriter, err := os.Pipe()
	if err != nil {
		return nil, 0, errors.Join(err, outputReader.Close(), outputWriter.Close())
	}
	events, control, err := os.Pipe()
	if err != nil {
		return nil, 0, errors.Join(err, outputReader.Close(), outputWriter.Close(), commandReader.Close(), commandWriter.Close())
	}
	devNull, err := os.Open("/dev/null")
	if err != nil {
		return nil, 0, errors.Join(err, outputReader.Close(), outputWriter.Close(), commandReader.Close(), commandWriter.Close(), events.Close(), control.Close())
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, 0, errors.Join(err, devNull.Close(), outputReader.Close(), outputWriter.Close(), commandReader.Close(), commandWriter.Close(), events.Close(), control.Close())
	}
	lifetimeReader, lifetimeWriter, err := os.Pipe()
	if err != nil {
		return nil, 0, errors.Join(err, devNull.Close(), outputReader.Close(), outputWriter.Close(), commandReader.Close(), commandWriter.Close(), events.Close(), control.Close())
	}
	guardian := exec.Command(executable, internalGuardianArgument)
	guardian.Stdin = devNull
	guardian.Stdout = outputWriter
	guardian.Stderr = outputWriter
	guardian.ExtraFiles = []*os.File{commandReader, control, cwd, lifetimeReader}
	guardian.SysProcAttr = newSessionAttributes()
	if err := guardian.Start(); err != nil {
		return nil, 0, errors.Join(err, devNull.Close(), outputReader.Close(), outputWriter.Close(), commandReader.Close(), commandWriter.Close(), events.Close(), control.Close(), lifetimeReader.Close(), lifetimeWriter.Close())
	}
	childCloseErr := errors.Join(devNull.Close(), outputWriter.Close(), commandReader.Close(), control.Close(), lifetimeReader.Close())
	if childCloseErr != nil {
		return nil, 0, stopGuardianStartup(guardian, commandWriter, lifetimeWriter, events, outputReader, childCloseErr)
	}
	if err := writeFrame(commandWriter, frameStart, guardianStart{Command: request.Command, GraceMs: request.TerminationGraceMs}); err != nil {
		return nil, 0, stopGuardianStartup(guardian, commandWriter, lifetimeWriter, events, outputReader, err)
	}
	if err := commandWriter.Close(); err != nil {
		return nil, 0, stopGuardianStartup(guardian, commandWriter, lifetimeWriter, events, outputReader, err)
	}
	frame, err := readFrame(events)
	if err != nil {
		return nil, 0, stopGuardianStartup(guardian, nil, lifetimeWriter, events, outputReader, err)
	}
	var ready guardianReady
	if err := decodeFrame(frame, frameGuardianReady, &ready); err != nil {
		return nil, 0, stopGuardianStartup(guardian, nil, lifetimeWriter, events, outputReader, err)
	}
	if ready.GuardianPID != guardian.Process.Pid || ready.ProcessGroupID != ready.GuardianPID {
		return nil, 0, stopGuardianStartup(guardian, nil, lifetimeWriter, events, outputReader, ErrExecution)
	}
	if err := verifyStartedProcessGroup(ready.GuardianPID); err != nil {
		return nil, 0, stopGuardianStartup(guardian, nil, lifetimeWriter, events, outputReader, err)
	}
	birthIdentity, err := processBirthIdentity(ready.GuardianPID)
	if err != nil {
		return nil, 0, stopGuardianStartup(guardian, nil, lifetimeWriter, events, outputReader, err)
	}
	waited := make(chan shellWaitResult, 1)
	go readGuardianExit(events, waited)
	captured := make(chan captureEvent, 16)
	go captureMergedOutput(outputReader, captured)
	return &runningShell{
		command: guardian, processGroup: ready.ProcessGroupID, birthIdentity: birthIdentity,
		shellPID: ready.ShellPID, waited: waited, captured: captured,
		lifetime:  lifetimeWriter,
		hardTimer: time.NewTimer(time.Duration(request.HardTimeoutMs) * time.Millisecond),
	}, generated.TimestampUnixMs(time.Now().UnixMilli()), nil
}

func runGuardian(_ context.Context, commandInput *os.File, control *os.File, cwd *os.File, lifetime *os.File) error {
	frame, err := readFrame(commandInput)
	if err != nil {
		return err
	}
	var start guardianStart
	if err := decodeFrame(frame, frameStart, &start); err != nil {
		return err
	}
	if err := unix.Fchdir(fileFD(cwd)); err != nil {
		return fmt.Errorf("guardian cwd: %w", err)
	}
	unix.CloseOnExec(fileFD(control))
	unix.CloseOnExec(fileFD(cwd))
	unix.CloseOnExec(fileFD(lifetime))
	shellInput, shellWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	shell := exec.Command("/bin/bash", "-lc", guardianShellScript)
	shell.Env = nil
	shell.Stdin = os.Stdin
	shell.Stdout = os.Stdout
	shell.Stderr = os.Stderr
	shell.ExtraFiles = []*os.File{shellInput}
	terminated := make(chan os.Signal, 1)
	signal.Notify(terminated, syscall.SIGTERM)
	defer signal.Stop(terminated)
	if err := shell.Start(); err != nil {
		return errors.Join(err, shellInput.Close(), shellWriter.Close())
	}
	if err := shellInput.Close(); err != nil {
		return errors.Join(err, shell.Process.Kill(), shell.Wait(), shellWriter.Close())
	}
	if err := writeFull(shellWriter, []byte(start.Command)); err != nil {
		return errors.Join(err, shell.Process.Kill(), shell.Wait(), shellWriter.Close())
	}
	if err := shellWriter.Close(); err != nil {
		return errors.Join(err, shell.Process.Kill(), shell.Wait())
	}
	ready := guardianReady{GuardianPID: os.Getpid(), ShellPID: shell.Process.Pid, ProcessGroupID: unix.Getpgrp()}
	if err := writeFrame(control, frameGuardianReady, ready); err != nil {
		return errors.Join(err, shell.Process.Kill(), shell.Wait())
	}
	shellExited := make(chan shellExit, 1)
	go func() {
		waitErr := shell.Wait()
		exited, normalizeErr := normalizeShellExit(shell.ProcessState, waitErr)
		if normalizeErr == nil {
			shellExited <- exited
		}
		close(shellExited)
	}()
	lifetimeEnded := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(io.Discard, lifetime)
		lifetimeEnded <- struct{}{}
	}()
	select {
	case exited, ok := <-shellExited:
		if !ok {
			return guardianCleanup(time.Duration(start.GraceMs) * time.Millisecond)
		}
		if err := writeFrame(control, frameShellExited, exited); err != nil {
			return guardianCleanup(time.Duration(start.GraceMs) * time.Millisecond)
		}
		select {
		case <-lifetimeEnded:
		case <-terminated:
		}
	case <-lifetimeEnded:
	case <-terminated:
		grace := time.NewTimer(time.Duration(start.GraceMs) * time.Millisecond)
		select {
		case exited, ok := <-shellExited:
			if ok {
				_ = writeFrame(control, frameShellExited, exited)
			}
		case <-grace.C:
		}
		if !grace.Stop() {
			select {
			case <-grace.C:
			default:
			}
		}
	}
	return guardianCleanup(time.Duration(start.GraceMs) * time.Millisecond)
}

func guardianCleanup(grace time.Duration) error {
	termErr := signalProcessGroup(unix.Getpgrp(), unix.SIGTERM)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	<-timer.C
	return errors.Join(termErr, signalProcessGroup(unix.Getpgrp(), unix.SIGKILL))
}

func normalizeShellExit(processState *os.ProcessState, waitErr error) (shellExit, error) {
	status, ok := processState.Sys().(syscall.WaitStatus)
	if !ok {
		return shellExit{}, ErrExecution
	}
	if status.Signaled() {
		signal := int(status.Signal())
		return shellExit{Signal: &signal}, nil
	}
	if status.Exited() {
		exitCode := status.ExitStatus()
		return shellExit{ExitCode: &exitCode}, nil
	}
	return shellExit{}, errors.Join(waitErr, ErrExecution)
}

func readGuardianExit(events *os.File, waited chan<- shellWaitResult) {
	defer events.Close()
	frame, err := readFrame(events)
	if err != nil {
		waited <- shellWaitResult{err: err}
		return
	}
	var exited shellExit
	if err := decodeFrame(frame, frameShellExited, &exited); err != nil {
		waited <- shellWaitResult{err: err}
		return
	}
	waited <- shellWaitResult{exitCode: exited.ExitCode, signal: exited.Signal}
}

func stopGuardianStartup(guardian *exec.Cmd, commandWriter *os.File, lifetimeWriter *os.File, events *os.File, outputReader *os.File, cause error) error {
	var closeErr error
	if commandWriter != nil {
		closeErr = commandWriter.Close()
	}
	return errors.Join(cause, closeErr, lifetimeWriter.Close(), events.Close(), outputReader.Close(), signalProcessGroup(guardian.Process.Pid, unix.SIGKILL), reapGuardian(guardian))
}

func reapGuardian(guardian *exec.Cmd) error {
	err := guardian.Wait()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return nil
	}
	return err
}

func (shell *runningShell) signalGroup(signal unix.Signal) error {
	if shell.command.ProcessState != nil {
		return ErrExecution
	}
	return signalProcessGroup(shell.processGroup, signal)
}

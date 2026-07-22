//go:build linux || darwin

package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

const internalBootstrapArgument = "--managed-bash-internal=bootstrap"

type frameResult struct {
	frame internalFrame
	err   error
}

func New(config Config) (*Manager, error) {
	if config.Executable == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable: %w", err)
		}
		config.Executable = executable
	}
	if config.StartupTimeout == 0 {
		config.StartupTimeout = defaultStartupTimeout
	}
	if config.TerminationGrace == 0 {
		config.TerminationGrace = defaultTerminationGrace
	}
	if config.PollInterval == 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.StateLockTimeout == 0 {
		config.StateLockTimeout = defaultStateLockTimeout
	}
	if config.StateLockPoll == 0 {
		config.StateLockPoll = defaultStateLockPoll
	}
	if config.StartupTimeout < time.Millisecond || config.TerminationGrace < time.Millisecond ||
		config.PollInterval < time.Millisecond || config.StateLockTimeout < time.Millisecond || config.StateLockPoll < time.Millisecond {
		return nil, ErrInvalidStart
	}
	contracts, err := contract.Load()
	if err != nil {
		return nil, fmt.Errorf("load contracts: %w", err)
	}
	return &Manager{config: config, contracts: contracts}, nil
}

func (manager *Manager) Start(
	ctx context.Context,
	request StartRequest,
) (generated.JobMetadata, error) {
	if err := ctx.Err(); err != nil {
		return generated.JobMetadata{}, err
	}
	internalRequest, err := manager.buildStartRequest(request)
	if err != nil {
		return generated.JobMetadata{}, err
	}
	workspace, cwd, err := openLaunchCapabilities(request.Invocation.WorkspacePath(), request.Invocation.Cwd())
	if err != nil {
		return generated.JobMetadata{}, err
	}
	defer workspace.Close()
	defer cwd.Close()
	if manager.afterCapabilitiesOpened != nil {
		manager.afterCapabilitiesOpened()
	}
	requestRead, requestWrite, err := os.Pipe()
	if err != nil {
		return generated.JobMetadata{}, fmt.Errorf("create bootstrap input: %w", err)
	}
	responseRead, responseWrite, err := os.Pipe()
	if err != nil {
		return generated.JobMetadata{}, errors.Join(fmt.Errorf("create bootstrap control: %w", err), requestRead.Close(), requestWrite.Close())
	}
	command := exec.Command(manager.config.Executable, internalBootstrapArgument)
	command.Stdin = requestRead
	command.ExtraFiles = []*os.File{responseWrite, workspace, cwd}
	if err := command.Start(); err != nil {
		return generated.JobMetadata{}, errors.Join(
			fmt.Errorf("start bootstrap: %w", err), requestRead.Close(), requestWrite.Close(),
			responseRead.Close(), responseWrite.Close(),
		)
	}
	closeErr := errors.Join(requestRead.Close(), responseWrite.Close())
	if closeErr != nil {
		return generated.JobMetadata{}, manager.stopBootstrap(command, fmt.Errorf("close bootstrap child descriptors: %w", closeErr))
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	timer := time.NewTimer(manager.config.StartupTimeout)
	defer timer.Stop()
	if err := writeFrameBefore(ctx, requestWrite, frameStart, internalRequest, timer.C); err != nil {
		return generated.JobMetadata{}, manager.stopBootstrapWithWait(command, waited, errors.Join(err, requestWrite.Close(), responseRead.Close()))
	}
	return startupHandshake{
		manager: manager, ctx: ctx, request: request, jobID: internalRequest.JobID, command: command, waited: waited,
		input: requestWrite, response: responseRead, workspace: workspace, timeout: timer.C,
	}.complete()
}

func writeFrameBefore(ctx context.Context, writer *os.File, kind frameKind, value any, timeout <-chan time.Time) error {
	written := make(chan error, 1)
	go func() { written <- writeFrame(writer, kind, value) }()
	select {
	case err := <-written:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout:
		return ErrStartupTimeout
	}
}

func (manager *Manager) buildStartRequest(request StartRequest) (internalStartRequest, error) {
	if request.Command == "" || len(request.Command) > 65536 {
		return internalStartRequest{}, ErrInvalidStart
	}
	hardTimeout := request.HardTimeout
	if hardTimeout == 0 {
		hardTimeout = time.Duration(manager.contracts.Policy().HardTimeout()) * time.Millisecond
	}
	hardTimeoutMs := hardTimeout.Milliseconds()
	if hardTimeoutMs < 1 || hardTimeoutMs > 86400000 {
		return internalStartRequest{}, ErrInvalidStart
	}
	outputLimit := request.OutputLimitBytes
	if outputLimit == 0 {
		outputLimit = int(manager.contracts.Policy().CaptureLimit())
	}
	if outputLimit < 1 || state.ByteCount(outputLimit) > manager.contracts.Policy().CaptureLimit() {
		return internalStartRequest{}, ErrInvalidStart
	}
	jobID, err := newJobID()
	if err != nil {
		return internalStartRequest{}, err
	}
	return internalStartRequest{
		JobID: jobID, SessionID: request.Invocation.SessionID(), WorkspacePath: request.Invocation.WorkspacePath(),
		Cwd: request.Invocation.Cwd(), Command: request.Command, CreatedAtUnixMs: generated.TimestampUnixMs(time.Now().UnixMilli()),
		HardTimeoutMs: generated.TimeoutMs(hardTimeoutMs), OutputLimitBytes: outputLimit,
		StartupTimeoutMs:   manager.config.StartupTimeout.Milliseconds(),
		TerminationGraceMs: manager.config.TerminationGrace.Milliseconds(),
		PollIntervalMs:     manager.config.PollInterval.Milliseconds(),
	}, nil
}

func (manager *Manager) stopBootstrap(command *exec.Cmd, cause error) error {
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	return manager.stopBootstrapWithWait(command, waited, cause)
}

func (*Manager) stopBootstrapWithWait(command *exec.Cmd, waited <-chan error, cause error) error {
	killErr := command.Process.Kill()
	waitErr := <-waited
	return errors.Join(cause, killErr, waitErr)
}

func newJobID() (generated.JobID, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate job id: %w", err)
	}
	return generated.JobID("job-" + hex.EncodeToString(random)), nil
}

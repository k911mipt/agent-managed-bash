package runner

import (
	"errors"
	"fmt"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

var (
	ErrActiveJob               = errors.New("runner store: job is active")
	ErrCorruptState            = errors.New("runner store: corrupt state")
	ErrInvalidJobID            = errors.New("runner store: invalid job id")
	ErrInvalidRuntime          = errors.New("runner store: invalid runtime metadata")
	ErrInvalidCursor           = errors.New("runner store: invalid cursor")
	ErrInvalidRange            = errors.New("runner store: invalid output range")
	ErrInvalidStateUpdate      = errors.New("runner store: invalid state update")
	ErrJobExists               = errors.New("runner store: job exists")
	ErrJobNotFound             = errors.New("runner store: job not found")
	ErrRunnerActive            = errors.New("runner store: runner is active")
	ErrExecution               = errors.New("runner: execution failed")
	ErrInternalProtocol        = errors.New("runner: internal protocol failure")
	ErrInvalidStart            = errors.New("runner: invalid start request")
	ErrStartupFailed           = errors.New("runner: startup failed")
	ErrStartupTimeout          = errors.New("runner: startup timeout")
	ErrCommitDurabilityUnknown = errors.New("runner: commit durability acknowledgement missing")
	ErrStateLockTimeout        = errors.New("runner store: state lock timeout")
	ErrUnsafeFilesystem        = errors.New("runner store: unsafe filesystem entry")
	ErrUnauthorized            = errors.New("runner store: unauthorized")
	ErrUnsupported             = errors.New("runner store: unsupported platform")
)

type CommitDurabilityError struct {
	JobID generated.JobID
	Cause error
}

func (err *CommitDurabilityError) Error() string {
	return fmt.Sprintf("job %s published without durability confirmation: %v", err.JobID, err.Cause)
}

func (err *CommitDurabilityError) Unwrap() error {
	return err.Cause
}

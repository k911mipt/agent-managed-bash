package runner

import (
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
)

const (
	defaultStartupTimeout   = 5 * time.Second
	defaultTerminationGrace = 10 * time.Second
	defaultPollInterval     = 100 * time.Millisecond
	defaultStateLockTimeout = 5 * time.Second
	defaultStateLockPoll    = 10 * time.Millisecond
)

type Config struct {
	Executable       string
	StartupTimeout   time.Duration
	TerminationGrace time.Duration
	PollInterval     time.Duration
	StateLockTimeout time.Duration
	StateLockPoll    time.Duration
}

type StartRequest struct {
	Invocation       state.TrustedInvocation
	Command          string
	HardTimeout      time.Duration
	OutputLimitBytes int
}

type Manager struct {
	config                  Config
	contracts               contract.Contracts
	beforeCommit            func()
	afterCommit             func()
	beforeOutputRead        func()
	afterListEntries        func()
	afterCapabilitiesOpened func()
}

type internalStartRequest struct {
	JobID              generated.JobID           `json:"job_id"`
	SessionID          generated.SessionID       `json:"session_id"`
	WorkspacePath      string                    `json:"workspace_path"`
	Cwd                string                    `json:"cwd"`
	Command            string                    `json:"command"`
	CreatedAtUnixMs    generated.TimestampUnixMs `json:"created_at_unix_ms"`
	HardTimeoutMs      generated.TimeoutMs       `json:"hard_timeout_ms"`
	OutputLimitBytes   int                       `json:"output_limit_bytes"`
	StartupTimeoutMs   int64                     `json:"startup_timeout_ms"`
	TerminationGraceMs int64                     `json:"termination_grace_ms"`
	PollIntervalMs     int64                     `json:"poll_interval_ms"`
}

type internalCommitted struct {
	Job             generated.JobMetadata `json:"job"`
	DurabilityError string                `json:"durability_error,omitempty"`
}

type internalFailure struct {
	Message string `json:"message"`
}

package runner

import (
	"os"
	"sync"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

type RuntimeMetadata struct {
	RunnerPID             int    `json:"runner_pid"`
	ShellPID              int    `json:"shell_pid"`
	ProcessGroupID        int    `json:"process_group_id"`
	ProcessGroupLeaderPID int    `json:"process_group_leader_pid"`
	ProcessBirthIdentity  string `json:"process_birth_identity"`
}

type Snapshot struct {
	State   generated.PersistedJobState
	Runtime RuntimeMetadata
}

type outputAppend struct {
	AcceptedBytes int
	LimitReached  bool
}

type Store struct {
	jobs                       *os.File
	workspace                  string
	cwd                        string
	sessionID                  generated.SessionID
	contracts                  contract.Contracts
	syncJobs                   func() error
	syncDirectory              func(*os.File) error
	closeJob                   func(*os.File) error
	lockTimeout                time.Duration
	lockPoll                   time.Duration
	afterTerminalIntent        func()
	beforeTerminalRecoveryLock func()
	afterRecoveryLock          func()
	beforeActiveMutation       func()
	afterMutationLock          func()
	beforeOutputRead           func()
	afterListEntries           func()
}

type pendingJob struct {
	mu      sync.Mutex
	store   *Store
	dir     *os.File
	name    string
	jobID   generated.JobID
	lease   *runnerLease
	settled bool
}

type runnerLease struct {
	mu    sync.Mutex
	file  *os.File
	jobID generated.JobID
	store *Store
}

type lockedJob struct {
	dir       *os.File
	stateLock *os.File
	state     generated.PersistedJobState
}

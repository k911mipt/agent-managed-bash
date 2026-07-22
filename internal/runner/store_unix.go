//go:build linux || darwin

package runner

import (
	"errors"
	"fmt"
	"os"

	"github.com/k911mipt/agent-managed-bash/internal/contract"
	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"golang.org/x/sys/unix"
)

func OpenStore(invocation state.TrustedInvocation, contracts contract.Contracts) (*Store, error) {
	workspace, err := openWorkspaceDirectory(invocation.WorkspacePath())
	if err != nil {
		return nil, err
	}
	store, openErr := OpenStoreAt(invocation, contracts, workspace)
	closeErr := workspace.Close()
	if openErr != nil || closeErr != nil {
		if store != nil {
			closeErr = errors.Join(closeErr, store.Close())
		}
		return nil, errors.Join(openErr, closeErr)
	}
	return store, nil
}

func OpenStoreAt(invocation state.TrustedInvocation, contracts contract.Contracts, workspace *os.File) (*Store, error) {
	if err := validateDirectory(workspace, false); err != nil {
		return nil, err
	}
	managed, err := ensurePrivateDirectory(workspace, ".managed_bash")
	if err != nil {
		return nil, err
	}
	jobs, err := ensurePrivateDirectory(managed, "jobs")
	closeErr := managed.Close()
	if err != nil || closeErr != nil {
		if jobs != nil {
			closeErr = errors.Join(closeErr, jobs.Close())
		}
		return nil, errors.Join(err, closeErr)
	}
	store := &Store{
		jobs: jobs, workspace: invocation.WorkspacePath(), cwd: invocation.Cwd(),
		sessionID: invocation.SessionID(), contracts: contracts,
	}
	store.syncJobs = jobs.Sync
	store.closeJob = func(file *os.File) error { return file.Close() }
	store.lockTimeout = defaultStateLockTimeout
	store.lockPoll = defaultStateLockPoll
	return store, nil
}

func (store *Store) Close() error {
	return store.jobs.Close()
}

func (store *Store) prepare(initial generated.PersistedJobState, runtime RuntimeMetadata) (*pendingJob, error) {
	if !validJobID(initial.Job.JobID) {
		return nil, ErrInvalidJobID
	}
	if initial.Job.Status != generated.JobStatusRunning || initial.Job.CapturedBytes != 0 {
		return nil, ErrInvalidStateUpdate
	}
	if initial.Session.SessionID != store.sessionID || initial.Job.Cwd != store.cwd {
		return nil, ErrInvalidStateUpdate
	}
	stateRaw, err := store.encodeState(initial)
	if err != nil {
		return nil, err
	}
	runtimeRaw, err := encodeRuntime(runtime)
	if err != nil {
		return nil, err
	}
	exists, err := entryExists(store.jobs, string(initial.Job.JobID))
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrJobExists
	}
	pendingName := ".starting-" + string(initial.Job.JobID)
	directory, err := createPrivateDirectory(store.jobs, pendingName)
	if err != nil {
		return nil, err
	}
	cleanup := func(cause error) error {
		return errors.Join(cause, removeDirectoryContents(directory), directory.Close(), removeDirectory(store.jobs, pendingName))
	}
	for _, name := range []string{"state.lock", "runner.lock", "output.log"} {
		if err := createSyncedFile(directory, name); err != nil {
			return nil, cleanup(err)
		}
	}
	if err := writeAtomicFile(directory, "runtime.json", runtimeRaw); err != nil {
		return nil, cleanup(err)
	}
	if err := writeAtomicFile(directory, "state.json", stateRaw); err != nil {
		return nil, cleanup(err)
	}
	runnerLock, err := openPrivateFileAt(directory, "runner.lock", unix.O_RDWR)
	if err != nil {
		return nil, cleanup(err)
	}
	if err := lockFile(runnerLock, true); err != nil {
		return nil, cleanup(errors.Join(err, runnerLock.Close()))
	}
	lease := &runnerLease{file: runnerLock, jobID: initial.Job.JobID, store: store}
	return &pendingJob{
		store: store, dir: directory, name: pendingName, jobID: initial.Job.JobID, lease: lease,
	}, nil
}

func (pending *pendingJob) commit() (*runnerLease, error) {
	pending.mu.Lock()
	defer pending.mu.Unlock()
	if pending.settled {
		return nil, ErrJobExists
	}
	exists, err := entryExists(pending.store.jobs, string(pending.jobID))
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrJobExists
	}
	if err := unix.Renameat(fileFD(pending.store.jobs), pending.name, fileFD(pending.store.jobs), string(pending.jobID)); err != nil {
		return nil, fmt.Errorf("commit job %s: %w", pending.jobID, err)
	}
	pending.settled = true
	syncErr := pending.store.syncJobs()
	closeErr := pending.store.closeJob(pending.dir)
	pending.dir = nil
	return pending.lease, errors.Join(syncErr, closeErr)
}

func (pending *pendingJob) abort() error {
	pending.mu.Lock()
	defer pending.mu.Unlock()
	if pending.settled {
		return nil
	}
	pending.settled = true
	err := pending.lease.release()
	err = errors.Join(err, removeDirectoryContents(pending.dir), pending.dir.Close())
	pending.dir = nil
	return errors.Join(err, removeDirectory(pending.store.jobs, pending.name))
}

func (lease *runnerLease) release() error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.releaseLocked()
}

func (lease *runnerLease) releaseLocked() error {
	if lease.file == nil {
		return nil
	}
	err := errors.Join(unlockFile(lease.file), lease.file.Close())
	lease.file = nil
	return err
}

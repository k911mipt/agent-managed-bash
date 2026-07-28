//go:build linux || darwin

package runner

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"golang.org/x/sys/unix"
)

type snapshotJob struct {
	dir   *os.File
	state generated.PersistedJobState
}

func (store *Store) openSnapshotJob(jobID generated.JobID) (*snapshotJob, error) {
	if !validJobID(jobID) {
		return nil, ErrInvalidJobID
	}
	directory, err := openDirectoryAt(store.jobs, string(jobID), true)
	if err != nil {
		return nil, err
	}
	stored, err := store.readState(directory, jobID)
	if err != nil {
		return nil, errors.Join(err, directory.Close())
	}
	return &snapshotJob{dir: directory, state: stored}, nil
}

func (store *Store) readState(directory *os.File, jobID generated.JobID) (generated.PersistedJobState, error) {
	raw, err := store.readAtomicSnapshotFileAt(directory, "state.json", maximumStateBytes)
	if err != nil {
		return generated.PersistedJobState{}, err
	}
	stored, decision := store.contracts.StateValidator().ValidateStored(raw, store.workspace)
	if !decision.Allowed || stored.Job.JobID != jobID {
		return generated.PersistedJobState{}, ErrCorruptState
	}
	return stored, nil
}

func (store *Store) readAtomicSnapshotFileAt(directory *os.File, name string, maximumBytes int64) ([]byte, error) {
	fd, err := unix.Openat(fileFD(directory), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, classifyOpenError("open file "+name, err)
	}
	file, err := fileFromFD(fd, name)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fileFD(file), &stat); err != nil {
		return nil, errors.Join(fmt.Errorf("inspect file: %w", err), store.closeJob(file))
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(os.Geteuid()) ||
		stat.Mode&0o777 != privateFileMode || stat.Nlink > 1 {
		return nil, errors.Join(ErrUnsafeFilesystem, store.closeJob(file))
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	if readErr != nil {
		return nil, errors.Join(fmt.Errorf("read %s: %w", name, readErr), store.closeJob(file))
	}
	if closeErr := store.closeJob(file); closeErr != nil {
		return nil, fmt.Errorf("close %s: %w", name, closeErr)
	}
	if int64(len(raw)) > maximumBytes {
		return nil, fmt.Errorf("read %s: %w", name, ErrUnsafeFilesystem)
	}
	return raw, nil
}

func (job *snapshotJob) close() error {
	return job.dir.Close()
}

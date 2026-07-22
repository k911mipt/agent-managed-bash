//go:build linux || darwin

package runner

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"golang.org/x/sys/unix"
)

func (store *Store) appendOutput(jobID generated.JobID, incoming []byte) (result outputAppend, err error) {
	job, err := store.openLockedJob(jobID)
	if err != nil {
		return outputAppend{}, err
	}
	defer func() {
		err = errors.Join(err, job.close())
	}()
	if job.state.Job.Status != generated.JobStatusRunning {
		return outputAppend{}, ErrInvalidStateUpdate
	}
	accepted, decision := store.contracts.Policy().AcceptedIncomingPrefix(
		state.ByteCount(job.state.Job.CapturedBytes), incoming, state.ByteCount(job.state.Job.OutputLimitBytes),
	)
	if !decision.Allowed {
		return outputAppend{}, ErrCorruptState
	}
	newCaptured := int(job.state.Job.CapturedBytes) + len(accepted)
	result = outputAppend{
		AcceptedBytes: len(accepted), LimitReached: newCaptured == job.state.Job.OutputLimitBytes,
	}
	output, err := openPrivateFileAt(job.dir, "output.log", unix.O_RDWR)
	if err != nil {
		return outputAppend{}, err
	}
	if err := requireCommittedOutput(output, job.state.Job.CapturedBytes); err != nil {
		return outputAppend{}, errors.Join(err, output.Close())
	}
	if len(accepted) == 0 {
		return result, output.Close()
	}
	if err := writeAt(output, accepted, int64(job.state.Job.CapturedBytes)); err != nil {
		return outputAppend{}, errors.Join(err, output.Close())
	}
	if err := output.Sync(); err != nil {
		return outputAppend{}, errors.Join(fmt.Errorf("sync output: %w", err), output.Close())
	}
	if err := output.Close(); err != nil {
		return outputAppend{}, fmt.Errorf("close output: %w", err)
	}
	next := job.state
	next.Job.CapturedBytes = generated.ByteCursor(newCaptured)
	raw, err := store.encodeState(next)
	if err != nil {
		return outputAppend{}, err
	}
	if err := writeAtomicFile(job.dir, "state.json", raw); err != nil {
		return outputAppend{}, err
	}
	return result, nil
}

func (store *Store) ReadOutput(jobID generated.JobID) (output []byte, err error) {
	job, err := store.openLockedJob(jobID)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, job.close())
	}()
	return readOutputLocked(job)
}

func readOutputLocked(job *lockedJob) ([]byte, error) {
	file, err := openPrivateFileAt(job.dir, "output.log", unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	if err := requireCommittedOutput(file, job.state.Job.CapturedBytes); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	output := make([]byte, int(job.state.Job.CapturedBytes))
	_, readErr := io.ReadFull(io.NewSectionReader(file, 0, int64(len(output))), output)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(ErrCorruptState, fmt.Errorf("read output: %w", readErr), closeErr)
	}
	return output, nil
}

func writeAt(file io.WriterAt, raw []byte, offset int64) error {
	for len(raw) > 0 {
		written, err := file.WriteAt(raw, offset)
		if err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		raw = raw[written:]
		offset += int64(written)
	}
	return nil
}

func requireCommittedOutput(file *os.File, captured generated.ByteCursor) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect output: %w", err)
	}
	if info.Size() < int64(captured) {
		return ErrCorruptState
	}
	return nil
}

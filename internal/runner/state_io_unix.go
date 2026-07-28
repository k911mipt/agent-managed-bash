//go:build linux || darwin

package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/state"
	"golang.org/x/sys/unix"
)

const (
	maximumStateBytes   = 2 << 20
	maximumRuntimeBytes = 16 << 10
)

func (store *Store) openLockedJob(jobID generated.JobID) (*lockedJob, error) {
	return store.openLockedJobContext(context.Background(), jobID)
}

func (store *Store) openLockedJobContext(ctx context.Context, jobID generated.JobID) (*lockedJob, error) {
	return store.openLockedJobWith(jobID, func(file *os.File) error {
		return lockStateFile(ctx, file, store.lockTimeout, store.lockPoll)
	})
}

func (store *Store) openLockedExecutionTerminalJob(jobID generated.JobID) (*lockedJob, error) {
	return store.openLockedJobWith(jobID, store.acquireTerminalStateLock)
}

func (store *Store) openLockedJobWith(jobID generated.JobID, acquireLock func(*os.File) error) (*lockedJob, error) {
	if !validJobID(jobID) {
		return nil, ErrInvalidJobID
	}
	directory, err := openDirectoryAt(store.jobs, string(jobID), true)
	if err != nil {
		return nil, err
	}
	stateLock, err := openPrivateFileAt(directory, "state.lock", unix.O_RDWR)
	if err != nil {
		return nil, errors.Join(err, directory.Close())
	}
	if err := acquireLock(stateLock); err != nil {
		return nil, errors.Join(err, stateLock.Close(), directory.Close())
	}
	raw, err := readPrivateFileAt(directory, "state.json", maximumStateBytes)
	if err != nil {
		return nil, errors.Join(err, unlockFile(stateLock), stateLock.Close(), directory.Close())
	}
	stored, decision := store.contracts.StateValidator().ValidateStored(raw, store.workspace)
	if !decision.Allowed || stored.Job.JobID != jobID {
		return nil, errors.Join(ErrCorruptState, unlockFile(stateLock), stateLock.Close(), directory.Close())
	}
	return &lockedJob{dir: directory, stateLock: stateLock, state: stored}, nil
}

func (job *lockedJob) close() error {
	return errors.Join(unlockFile(job.stateLock), job.stateLock.Close(), job.dir.Close())
}

func (store *Store) encodeState(persisted generated.PersistedJobState) ([]byte, error) {
	raw, err := json.Marshal(persisted)
	if err != nil {
		return nil, fmt.Errorf("encode state: %w", err)
	}
	validated, decision := store.contracts.StateValidator().ValidateStored(raw, store.workspace)
	if !decision.Allowed || !reflect.DeepEqual(validated, persisted) {
		return nil, ErrCorruptState
	}
	return raw, nil
}

func readRuntime(directory *os.File) (RuntimeMetadata, error) {
	raw, err := readPrivateFileAt(directory, "runtime.json", maximumRuntimeBytes)
	if err != nil {
		return RuntimeMetadata{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var runtime RuntimeMetadata
	if err := decoder.Decode(&runtime); err != nil {
		return RuntimeMetadata{}, errors.Join(ErrInvalidRuntime, fmt.Errorf("decode runtime metadata: %w", err))
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return RuntimeMetadata{}, ErrInvalidRuntime
	}
	if err := validateRuntime(runtime); err != nil {
		return RuntimeMetadata{}, err
	}
	return runtime, nil
}

func encodeRuntime(runtime RuntimeMetadata) ([]byte, error) {
	if err := validateRuntime(runtime); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(runtime)
	if err != nil {
		return nil, fmt.Errorf("encode runtime metadata: %w", err)
	}
	return raw, nil
}

func validateRuntime(runtime RuntimeMetadata) error {
	if runtime.RunnerPID <= 0 || runtime.ShellPID <= 0 || runtime.ProcessGroupID <= 0 ||
		runtime.ProcessGroupLeaderPID != runtime.ProcessGroupID || runtime.ProcessBirthIdentity == "" ||
		len(runtime.ProcessBirthIdentity) > 4096 {
		return ErrInvalidRuntime
	}
	return nil
}

func readPrivateFileAt(directory *os.File, name string, maximumBytes int64) ([]byte, error) {
	file, err := openPrivateFileAt(directory, name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(fmt.Errorf("read %s: %w", name, readErr), closeErr)
	}
	if int64(len(raw)) > maximumBytes {
		return nil, fmt.Errorf("read %s: %w", name, ErrUnsafeFilesystem)
	}
	return raw, nil
}

func writeAtomicFile(directory *os.File, target string, raw []byte) (err error) {
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Errorf("generate temporary name: %w", err)
	}
	temporary := "." + target + "." + hex.EncodeToString(suffix)
	file, err := createPrivateFileAt(directory, temporary)
	if err != nil {
		return err
	}
	temporaryExists := true
	defer func() {
		if temporaryExists {
			err = errors.Join(err, unlinkTemporary(directory, temporary))
		}
	}()
	written, writeErr := io.Copy(file, bytes.NewReader(raw))
	if writeErr != nil || written != int64(len(raw)) {
		return errors.Join(fmt.Errorf("write %s: %w", temporary, errors.Join(writeErr, io.ErrShortWrite)), file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync %s: %w", temporary, err), file.Close())
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", temporary, err)
	}
	exists, err := entryExists(directory, target)
	if err != nil {
		return err
	}
	if exists {
		existing, err := openPrivateFileAt(directory, target, unix.O_RDONLY)
		if err != nil {
			return err
		}
		if err := existing.Close(); err != nil {
			return fmt.Errorf("close existing %s: %w", target, err)
		}
	}
	if err := unix.Renameat(fileFD(directory), temporary, fileFD(directory), target); err != nil {
		return fmt.Errorf("publish %s: %w", target, err)
	}
	temporaryExists = false
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync published %s: %w", target, err)
	}
	return nil
}

func unlinkTemporary(directory *os.File, name string) error {
	if err := unix.Unlinkat(fileFD(directory), name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("remove temporary %s: %w", name, err)
	}
	return nil
}

func validJobID(jobID generated.JobID) bool {
	if len(jobID) == 0 || len(jobID) > 64 {
		return false
	}
	for _, character := range jobID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func sameImmutableStateFields(current generated.PersistedJobState, next generated.PersistedJobState) bool {
	return current.SchemaVersion == next.SchemaVersion && current.Session == next.Session &&
		current.Job.JobID == next.Job.JobID && current.Job.OwnerSessionID == next.Job.OwnerSessionID &&
		current.Job.WorkspacePath == next.Job.WorkspacePath && current.Job.Cwd == next.Job.Cwd &&
		current.Job.Command == next.Job.Command && current.Job.CreatedAtUnixMs == next.Job.CreatedAtUnixMs &&
		current.Job.StartedAtUnixMs == next.Job.StartedAtUnixMs &&
		current.Job.HardTimeoutMs == next.Job.HardTimeoutMs &&
		current.Job.OutputLimitBytes == next.Job.OutputLimitBytes
}

func transitionAllowed(policy state.Policy, current generated.PersistedJobState, next generated.PersistedJobState) bool {
	if current.Job.Status == next.Job.Status {
		return true
	}
	return policy.AuthorizeTransition(current.Job.Status, next.Job.Status).Allowed
}

func preservesPublishedFields(current generated.PersistedJobState, next generated.PersistedJobState) bool {
	if current.Result != nil && !reflect.DeepEqual(current.Result, next.Result) {
		return false
	}
	if current.Cancellation != nil && !reflect.DeepEqual(current.Cancellation, next.Cancellation) {
		return false
	}
	if current.Job.FinishedAtUnixMs != nil && !reflect.DeepEqual(current.Job.FinishedAtUnixMs, next.Job.FinishedAtUnixMs) {
		return false
	}
	return true
}

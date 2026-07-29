//go:build linux || darwin

package runner_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"golang.org/x/sys/unix"
)

const diagnosticOutputLimit = 256 << 10

func dumpLifecycleFailure(t *testing.T, snapshot runner.Snapshot) {
	t.Helper()
	state := snapshot.State
	job := state.Job
	t.Logf(
		"LIFECYCLE_DIAG snapshot job_id=%s status=%s captured_bytes=%d hard_timeout_ms=%d output_limit_bytes=%d result=%+v cancellation=%+v cursors=%+v",
		job.JobID,
		job.Status,
		job.CapturedBytes,
		job.HardTimeoutMs,
		job.OutputLimitBytes,
		state.Result,
		state.Cancellation,
		state.Observers,
	)
	jobDirectory := filepath.Join(job.WorkspacePath, ".managed_bash", "jobs", string(job.JobID))
	logLifecycleFiles(t, jobDirectory)
	for _, name := range []string{"runner.lock", "state.lock", "recovery.lock"} {
		status, err := probeLifecycleLock(filepath.Join(jobDirectory, name))
		t.Logf("LIFECYCLE_DIAG lock name=%s status=%s error=%v", name, status, err)
	}
	runtimeMetadata := snapshot.Runtime
	identityVerified, identityErr := runner.VerifyProcessIdentity(
		runtimeMetadata.ProcessGroupLeaderPID,
		runtimeMetadata.ProcessBirthIdentity,
	)
	t.Logf(
		"LIFECYCLE_DIAG runtime runner_pid=%d shell_pid=%d process_group_id=%d process_group_leader_pid=%d process_birth_identity=%q identity_verified=%t identity_error=%v",
		runtimeMetadata.RunnerPID,
		runtimeMetadata.ShellPID,
		runtimeMetadata.ProcessGroupID,
		runtimeMetadata.ProcessGroupLeaderPID,
		runtimeMetadata.ProcessBirthIdentity,
		identityVerified,
		identityErr,
	)
	logLifecycleLiveness(t, runtimeMetadata)
	if runtime.GOOS == "darwin" {
		logDiagnosticCommand(t, "ps", "-axo", "pid=,ppid=,pgid=,sess=,stat=,etime=,time=,pcpu=,wchan=,command=")
	}
	logDiagnosticCommand(t, "lsof", "-nP", "-p", strconv.Itoa(runtimeMetadata.RunnerPID))
	logDiagnosticCommand(t, "lsof", "-nP", "-g", strconv.Itoa(runtimeMetadata.ProcessGroupID))
	logParentGoroutines(t)
	runnerRole, roleErr := processHasRole(runtimeMetadata.RunnerPID, "runner")
	runnerAliveErr := unix.Kill(runtimeMetadata.RunnerPID, 0)
	runnerLockStatus, runnerLockErr := probeLifecycleLock(filepath.Join(jobDirectory, "runner.lock"))
	t.Logf(
		"LIFECYCLE_DIAG runner_verification role=%t role_error=%v alive_error=%v runner_lock=%s runner_lock_error=%v",
		runnerRole,
		roleErr,
		runnerAliveErr,
		runnerLockStatus,
		runnerLockErr,
	)
	if identityVerified && identityErr == nil && runnerRole && runnerAliveErr == nil && runnerLockStatus == "held" && runnerLockErr == nil {
		signalErr := unix.Kill(runtimeMetadata.RunnerPID, unix.SIGQUIT)
		t.Logf("LIFECYCLE_DIAG runner_sigquit pid=%d error=%v", runtimeMetadata.RunnerPID, signalErr)
		if signalErr == nil {
			waitForRunnerLockFree(t, filepath.Join(jobDirectory, "runner.lock"), 2*time.Second)
		}
	}
	logChildJournals(t, os.Getenv(testDiagnosticsEnvironment))
}

func logLifecycleFiles(t *testing.T, jobDirectory string) {
	t.Helper()
	entries, err := os.ReadDir(jobDirectory)
	if err != nil {
		t.Logf("LIFECYCLE_DIAG directory path=%q error=%v", jobDirectory, err)
	} else {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		slices.Sort(names)
		t.Logf("LIFECYCLE_DIAG directory path=%q entries=%q", jobDirectory, names)
	}
	for _, name := range []string{"state.json", "runtime.json"} {
		raw, readErr := readDiagnosticFile(filepath.Join(jobDirectory, name))
		t.Logf("LIFECYCLE_DIAG file name=%s content=%q error=%v", name, raw, readErr)
	}
	outputInfo, outputErr := os.Stat(filepath.Join(jobDirectory, "output.log"))
	if outputErr != nil {
		t.Logf("LIFECYCLE_DIAG output error=%v", outputErr)
	} else {
		t.Logf("LIFECYCLE_DIAG output size=%d", outputInfo.Size())
	}
	_, intentErr := os.Lstat(filepath.Join(jobDirectory, "terminal.pending"))
	t.Logf("LIFECYCLE_DIAG terminal_pending exists=%t error=%v", intentErr == nil, intentErr)
}

func probeLifecycleLock(path string) (string, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return "error", err
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return "held", nil
		}
		return "error", err
	}
	return "free", unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func logLifecycleLiveness(t *testing.T, metadata runner.RuntimeMetadata) {
	t.Helper()
	for name, pid := range map[string]int{
		"runner":       metadata.RunnerPID,
		"shell":        metadata.ShellPID,
		"group_leader": metadata.ProcessGroupLeaderPID,
	} {
		t.Logf("LIFECYCLE_DIAG liveness target=%s pid=%d error=%v", name, pid, unix.Kill(pid, 0))
	}
	t.Logf(
		"LIFECYCLE_DIAG liveness target=process_group pgid=%d error=%v",
		metadata.ProcessGroupID,
		unix.Kill(-metadata.ProcessGroupID, 0),
	)
}

func processHasRole(pid int, role string) (bool, error) {
	output, err := diagnosticCommandOutput("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	if err != nil {
		return false, err
	}
	return strings.Contains(output, "--managed-bash-internal="+role), nil
}

func logDiagnosticCommand(t *testing.T, name string, args ...string) {
	t.Helper()
	output, err := diagnosticCommandOutput(name, args...)
	t.Logf("LIFECYCLE_DIAG command name=%s args=%q output=%q error=%v", name, args, output, err)
}

func diagnosticCommandOutput(name string, args ...string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var output diagnosticWriter
	command := exec.CommandContext(ctx, path, args...)
	command.Stdout = &output
	command.Stderr = &output
	err = command.Run()
	return output.String(), err
}

func logParentGoroutines(t *testing.T) {
	t.Helper()
	var output diagnosticWriter
	profile := pprof.Lookup("goroutine")
	if profile == nil {
		t.Logf("LIFECYCLE_DIAG parent_goroutines error=profile unavailable")
		return
	}
	err := profile.WriteTo(&output, 2)
	t.Logf("LIFECYCLE_DIAG parent_goroutines output=%q error=%v", output.String(), err)
}

func waitForRunnerLockFree(t *testing.T, path string, maximumWait time.Duration) {
	t.Helper()
	deadline := time.NewTimer(maximumWait)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := probeLifecycleLock(path)
		if status == "free" {
			t.Logf("LIFECYCLE_DIAG completion_fence runner_lock=free error=%v", err)
			return
		}
		select {
		case <-deadline.C:
			t.Logf("LIFECYCLE_DIAG completion_fence runner_lock=%s error=%v timeout=%s", status, err, maximumWait)
			return
		case <-ticker.C:
		}
	}
}

func logChildJournals(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Logf("LIFECYCLE_DIAG child_journals directory=%q error=%v", directory, err)
		return
	}
	for _, entry := range entries {
		raw, readErr := readDiagnosticFile(filepath.Join(directory, entry.Name()))
		t.Logf("LIFECYCLE_DIAG child_journal name=%s content=%q error=%v", entry.Name(), raw, readErr)
	}
}

func readDiagnosticFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, diagnosticOutputLimit+1))
	return boundedDiagnosticText(raw), err
}

func boundedDiagnosticText(raw []byte) string {
	if len(raw) <= diagnosticOutputLimit {
		return string(raw)
	}
	return string(raw[:diagnosticOutputLimit]) + "\n<truncated>"
}

func Test_logDiagnosticCommand_tolerates_missing_tool(t *testing.T) {
	logDiagnosticCommand(t, "amb-intentionally-missing-tool", "--version")
}

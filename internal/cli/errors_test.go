package cli

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/k911mipt/agent-managed-bash/internal/runner"
	"github.com/stretchr/testify/require"
)

func Test_failureFromError_maps_runner_errors_to_protocol_codes(t *testing.T) {
	tests := []struct {
		name     string
		action   generated.Action
		err      error
		expected generated.ErrorCode
	}{
		{name: "invalid start", action: generated.ActionRun, err: runner.ErrInvalidStart, expected: generated.ErrorCodeInvalidRequest},
		{name: "invalid cursor", action: generated.ActionWait, err: runner.ErrInvalidCursor, expected: generated.ErrorCodeInvalidCursor},
		{name: "invalid range", action: generated.ActionOutput, err: runner.ErrInvalidRange, expected: generated.ErrorCodeInvalidRange},
		{name: "missing job", action: generated.ActionStatus, err: runner.ErrJobNotFound, expected: generated.ErrorCodeJobNotFound},
		{name: "unauthorized", action: generated.ActionCancel, err: runner.ErrUnauthorized, expected: generated.ErrorCodeUnauthorized},
		{name: "unsafe filesystem", action: generated.ActionRun, err: runner.ErrUnsafeFilesystem, expected: generated.ErrorCodeUnauthorized},
		{name: "active job", action: generated.ActionRemove, err: runner.ErrActiveJob, expected: generated.ErrorCodeActiveJob},
		{name: "state lock timeout", action: generated.ActionStatus, err: runner.ErrStateLockTimeout, expected: generated.ErrorCodeConflict},
		{name: "corrupt state", action: generated.ActionStatus, err: runner.ErrCorruptState, expected: generated.ErrorCodeCorruptState},
		{name: "invalid runtime", action: generated.ActionStatus, err: runner.ErrInvalidRuntime, expected: generated.ErrorCodeCorruptState},
		{name: "startup timeout", action: generated.ActionRun, err: runner.ErrStartupTimeout, expected: generated.ErrorCodeRunnerUnavailable},
		{name: "unsupported platform", action: generated.ActionRun, err: runner.ErrUnsupported, expected: generated.ErrorCodeRunnerUnavailable},
		{name: "startup failure", action: generated.ActionRun, err: runner.ErrStartupFailed, expected: generated.ErrorCodeRunnerUnavailable},
		{name: "filesystem missing", action: generated.ActionRun, err: fs.ErrNotExist, expected: generated.ErrorCodeIoFailure},
		{name: "filesystem permission", action: generated.ActionRun, err: fs.ErrPermission, expected: generated.ErrorCodeIoFailure},
		{name: "path error", action: generated.ActionStatus, err: &os.PathError{Op: "open", Path: "/state", Err: syscall.EIO}, expected: generated.ErrorCodeIoFailure},
		{name: "syscall error", action: generated.ActionRun, err: &os.SyscallError{Syscall: "pipe", Err: syscall.EMFILE}, expected: generated.ErrorCodeIoFailure},
		{name: "disk full", action: generated.ActionStatus, err: syscall.ENOSPC, expected: generated.ErrorCodeIoFailure},
		{name: "short write", action: generated.ActionOutput, err: io.ErrShortWrite, expected: generated.ErrorCodeIoFailure},
		{name: "unmapped", action: generated.ActionStatus, err: errors.New("unexpected"), expected: generated.ErrorCodeInternal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// When
			problem := failureFromError(testCase.action, testCase.err)

			// Then
			require.Equal(t, testCase.expected, problem.code)
		})
	}
}

func Test_Diagnostic_is_bounded_to_one_line(t *testing.T) {
	// Given
	raw := strings.Repeat("diagnostic", 100) + "\r\nsecond line\x00"

	// When
	formatted := Diagnostic(errors.New(raw))

	// Then
	require.NotContains(t, formatted, "\r")
	require.NotContains(t, formatted, "\n")
	require.NotContains(t, formatted, "\x00")
	require.LessOrEqual(t, len([]rune(formatted)), 512)
}

func Test_Application_failure_diagnostic_does_not_echo_untrusted_cause(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	marker := "secret command text\nsecond line"
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	application.writeFailure(
		Streams{Stdout: &stdout, Stderr: &stderr},
		newFailure(generated.ActionRun, newProblem(generated.ErrorCodeInvalidRequest, errors.New(marker))),
	)

	// Then
	require.NotContains(t, stderr.String(), "secret command text")
	require.Equal(t, 1, strings.Count(stderr.String(), "\n"))
}

func Test_Application_writes_every_protocol_error_with_its_exit_class(t *testing.T) {
	// Given
	application, err := New(Config{BinaryVersion: "dev"})
	require.NoError(t, err)
	expectedExits := map[generated.ErrorCode]int{
		generated.ErrorCodeMalformedJson: 2, generated.ErrorCodeInvalidRequest: 2,
		generated.ErrorCodeIncompatibleVersion: 2, generated.ErrorCodeInvalidRange: 2,
		generated.ErrorCodeInvalidCursor: 2, generated.ErrorCodeJobNotFound: 3,
		generated.ErrorCodeUnauthorized: 3, generated.ErrorCodeActiveJob: 4,
		generated.ErrorCodeConflict: 4, generated.ErrorCodeCorruptState: 5,
		generated.ErrorCodeRunnerUnavailable: 5, generated.ErrorCodeIoFailure: 5,
		generated.ErrorCodeInternal: 5,
	}

	for code, expectedExit := range expectedExits {
		t.Run(string(code), func(t *testing.T) {
			// When
			exitCode, stdout, stderr := writeTestFailure(application, code)

			// Then
			require.Equal(t, expectedExit, exitCode)
			require.JSONEq(t, expectedErrorJSON(code), stdout)
			require.Contains(t, stderr, "code="+string(code))
		})
	}
}

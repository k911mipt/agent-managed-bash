//go:build linux || darwin

package runner_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testDiagnosticsEnvironment = "AMB_TEST_DIAGNOSTICS_DIR"
const testCrashChildEnvironment = "AMB_TEST_CRASH_CHILD"

type internalChildDiagnostics struct {
	role     string
	pid      int
	exitFile *os.File
	setupErr error
}

func internalChildRole(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	const prefix = "--managed-bash-internal="
	if !strings.HasPrefix(args[0], prefix) {
		return "", false
	}
	role := strings.TrimPrefix(args[0], prefix)
	switch role {
	case "bootstrap", "runner", "guardian":
		return role, true
	default:
		return "", false
	}
}

func childJournalName(kind string, role string, pid int) string {
	return fmt.Sprintf("%s-%s-%d.log", kind, role, pid)
}

func prepareInternalChildDiagnostics(args []string) internalChildDiagnostics {
	role, internal := internalChildRole(args)
	directory := os.Getenv(testDiagnosticsEnvironment)
	if !internal || directory == "" {
		return internalChildDiagnostics{}
	}
	diagnostics := internalChildDiagnostics{role: role, pid: os.Getpid()}
	root, err := os.OpenRoot(directory)
	if err != nil {
		diagnostics.setupErr = err
		return diagnostics
	}
	diagnostics.exitFile, err = root.OpenFile(childJournalName("exit", role, diagnostics.pid), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	diagnostics.setupErr = errors.Join(diagnostics.setupErr, err)
	crashFile, err := root.OpenFile(childJournalName("crash", role, diagnostics.pid), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	diagnostics.setupErr = errors.Join(diagnostics.setupErr, err)
	debug.SetTraceback("all")
	if crashFile != nil {
		diagnostics.setupErr = errors.Join(diagnostics.setupErr, debug.SetCrashOutput(crashFile, debug.CrashOptions{}), crashFile.Close())
	}
	diagnostics.setupErr = errors.Join(diagnostics.setupErr, root.Close())
	return diagnostics
}

func (diagnostics internalChildDiagnostics) recordDispatch(handled bool, dispatchErr error) {
	if diagnostics.exitFile == nil {
		return
	}
	errorValue := "nil"
	if dispatchErr != nil {
		errorValue = dispatchErr.Error()
	}
	_, writeErr := fmt.Fprintf(
		diagnostics.exitFile,
		"role=%s\npid=%d\nhandled=%t\nerror=%s\ndiagnostics_error=%v\n",
		diagnostics.role,
		diagnostics.pid,
		handled,
		errorValue,
		diagnostics.setupErr,
	)
	_ = errors.Join(writeErr, diagnostics.exitFile.Sync(), diagnostics.exitFile.Close())
}

func Test_internalChildRole_parses_supported_internal_mode(t *testing.T) {
	tests := []struct {
		argument string
		role     string
		ok       bool
	}{
		{argument: "--managed-bash-internal=bootstrap", role: "bootstrap", ok: true},
		{argument: "--managed-bash-internal=runner", role: "runner", ok: true},
		{argument: "--managed-bash-internal=guardian", role: "guardian", ok: true},
		{argument: "-test.run=TestSomething"},
	}
	for _, testCase := range tests {
		t.Run(testCase.argument, func(t *testing.T) {
			role, ok := internalChildRole([]string{testCase.argument})

			require.Equal(t, testCase.role, role)
			require.Equal(t, testCase.ok, ok)
		})
	}
}

func Test_childJournalName_builds_role_scoped_basename(t *testing.T) {
	require.Equal(t, "exit-runner-42.log", childJournalName("exit", "runner", 42))
	require.Equal(t, "crash-guardian-7.log", childJournalName("crash", "guardian", 7))
}

func Test_internalChildDiagnostics_is_inert_without_test_environment(t *testing.T) {
	t.Setenv(testDiagnosticsEnvironment, "")

	diagnostics := prepareInternalChildDiagnostics([]string{"--managed-bash-internal=runner"})

	require.Empty(t, diagnostics.role)
	require.Nil(t, diagnostics.exitFile)
}

func Test_diagnosticsEnvironment_is_configured_for_runner_children(t *testing.T) {
	require.DirExists(t, os.Getenv(testDiagnosticsEnvironment))
}

func Test_internalChildDiagnostics_writes_synchronous_exit_journal(t *testing.T) {
	directory := t.TempDir()
	t.Setenv(testDiagnosticsEnvironment, directory)
	t.Cleanup(func() { require.NoError(t, debug.SetCrashOutput(nil, debug.CrashOptions{})) })
	diagnostics := prepareInternalChildDiagnostics([]string{"--managed-bash-internal=runner"})

	diagnostics.recordDispatch(true, nil)

	exitPath := filepath.Join(directory, childJournalName("exit", "runner", os.Getpid()))
	raw, err := os.ReadFile(exitPath)
	require.NoError(t, err)
	require.Contains(t, string(raw), "role=runner\n")
	require.Contains(t, string(raw), "handled=true\n")
	require.Contains(t, string(raw), "error=nil\n")
	info, err := os.Stat(exitPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func Test_internalChildDiagnostics_captures_child_panic(t *testing.T) {
	directory := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.Command(executable, "-test.run=^$")
	command.Env = append(os.Environ(), testDiagnosticsEnvironment+"="+directory, testCrashChildEnvironment+"=1")

	err = command.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	crashPath := filepath.Join(directory, childJournalName("crash", "runner", command.Process.Pid))
	raw, readErr := os.ReadFile(crashPath)
	require.NoError(t, readErr)
	require.Contains(t, string(raw), "panic: intentional lifecycle diagnostic crash")
}

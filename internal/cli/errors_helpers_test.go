package cli

import (
	"bytes"
	"fmt"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

func writeTestFailure(application *Application, code generated.ErrorCode) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := application.writeFailure(Streams{Stdout: &stdout, Stderr: &stderr}, newFailure(
		generated.ActionVersion,
		newProblem(code, fmt.Errorf("test diagnostic")),
	))
	return exitCode, stdout.String(), stderr.String()
}

func expectedErrorJSON(code generated.ErrorCode) string {
	return fmt.Sprintf(
		`{"schema_version":1,"ok":false,"action":"version","error":{"code":%q,"message":%q}}`,
		code,
		publicErrorMessage(code),
	)
}

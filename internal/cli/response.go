package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

type responseInvariantError struct{ cause error }

func (err *responseInvariantError) Error() string { return err.cause.Error() }
func (err *responseInvariantError) Unwrap() error { return err.cause }

type responseWriteError struct{ cause error }

func (err *responseWriteError) Error() string { return err.cause.Error() }
func (err *responseWriteError) Unwrap() error { return err.cause }

func (application *Application) writeResponse(writer io.Writer, response any) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return &responseInvariantError{cause: fmt.Errorf("marshal response: %w", err)}
	}
	if err := application.validator.ValidateResponse(raw); err != nil {
		return &responseInvariantError{cause: fmt.Errorf("validate response: %w", err)}
	}
	raw = append(raw, '\n')
	written, err := writer.Write(raw)
	if err != nil {
		return &responseWriteError{cause: fmt.Errorf("write response: %w", err)}
	}
	if written != len(raw) {
		return &responseWriteError{cause: fmt.Errorf("write response: %w", io.ErrShortWrite)}
	}
	return nil
}

func (application *Application) writeSuccess(streams Streams, action generated.Action, response any) int {
	if err := application.writeResponse(streams.Stdout, response); err != nil {
		var invariantError *responseInvariantError
		if errors.As(err, &invariantError) {
			return application.writeFailure(streams, newFailure(action, newProblem(generated.ErrorCodeInternal, err)))
		}
		return application.writeOutputFailure(streams.Stderr, action, err)
	}
	return int(application.contracts.Policy().SuccessExitClass())
}

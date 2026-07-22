package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"unicode/utf8"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
)

const maxRequestBytes = 1 << 20

type requestEnvelope struct {
	Action        json.RawMessage `json:"action"`
	SchemaVersion json.RawMessage `json:"schema_version"`
}

type parsedRequest struct {
	value requestValue
}

func (application *Application) readRequest(selected generated.Action, reader io.Reader) (parsedRequest, *failure) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxRequestBytes+1))
	if err != nil {
		return parsedRequest{}, newFailure(selected, newProblem(generated.ErrorCodeIoFailure, fmt.Errorf("read stdin: %w", err)))
	}
	if len(raw) > maxRequestBytes {
		return parsedRequest{}, newFailure(selected, newProblem(generated.ErrorCodeInvalidRequest, fmt.Errorf("request exceeds %d bytes", maxRequestBytes)))
	}
	if len(raw) == 0 || !utf8.Valid(raw) || !json.Valid(raw) {
		return parsedRequest{}, newFailureWithoutAction(selected, newProblem(generated.ErrorCodeMalformedJson, fmt.Errorf("stdin does not contain one valid UTF-8 JSON value")))
	}

	var envelope requestEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return parsedRequest{}, newFailureWithoutAction(selected, newProblem(generated.ErrorCodeInvalidRequest, fmt.Errorf("decode request envelope: %w", err)))
	}
	action, actionKnown := parseRequestAction(envelope.Action)
	version, versionValid := parseSchemaVersion(envelope.SchemaVersion)
	if !versionValid {
		problem := newFailureWithoutAction(selected, newProblem(generated.ErrorCodeInvalidRequest, fmt.Errorf("schema_version must be an integer")))
		if actionKnown {
			problem = newFailure(action, problem)
		}
		return parsedRequest{}, problem
	}
	if version.Cmp(big.NewInt(1)) != 0 {
		details := &generated.ErrorDetails{
			Field: stringPointer("schema_version"), Expected: stringPointer("1"), Actual: stringPointer(version.String()),
		}
		problem := newFailureWithoutAction(selected, newDetailedProblem(generated.ErrorCodeIncompatibleVersion, details, fmt.Errorf("unsupported schema version %s", version)))
		if actionKnown {
			problem = newFailure(action, problem)
		}
		return parsedRequest{}, problem
	}
	if !actionKnown {
		return parsedRequest{}, newFailureWithoutAction(selected, newProblem(generated.ErrorCodeInvalidRequest, fmt.Errorf("action must be a known string")))
	}
	if action != selected {
		details := &generated.ErrorDetails{
			Field: stringPointer("action"), Expected: stringPointer(string(selected)), Actual: stringPointer(string(action)),
		}
		return parsedRequest{}, newFailure(action, newDetailedProblem(generated.ErrorCodeInvalidRequest, details, fmt.Errorf("request action differs from subcommand")))
	}
	if err := application.validator.ValidateRequest(raw); err != nil {
		return parsedRequest{}, newFailure(action, newProblem(generated.ErrorCodeInvalidRequest, err))
	}

	canonical, err := canonicalizeSchemaVersion(raw)
	if err != nil {
		return parsedRequest{}, newFailure(action, newProblem(generated.ErrorCodeInternal, err))
	}
	value, err := decodeValidatedRequest(action, canonical)
	if err != nil {
		return parsedRequest{}, newFailure(action, newProblem(generated.ErrorCodeInternal, err))
	}
	return parsedRequest{value: value}, nil
}

func parseRequestAction(raw json.RawMessage) (generated.Action, bool) {
	var action string
	if len(raw) == 0 || json.Unmarshal(raw, &action) != nil {
		return "", false
	}
	return knownAction(action)
}

func parseSchemaVersion(raw json.RawMessage) (*big.Int, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if decoder.Decode(&number) != nil {
		return nil, false
	}
	version, ok := new(big.Rat).SetString(number.String())
	if !ok || !version.IsInt() {
		return nil, false
	}
	return new(big.Int).Set(version.Num()), true
}

func canonicalizeSchemaVersion(raw []byte) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("decode validated request object: %w", err)
	}
	object["schema_version"] = json.RawMessage("1")
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("canonicalize schema version: %w", err)
	}
	return canonical, nil
}

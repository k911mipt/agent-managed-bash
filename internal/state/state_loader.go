package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	modelsSchemaURL = "https://agent-managed-bash.dev/schema/v1/models.schema.json"
	stateSchemaURL  = "https://agent-managed-bash.dev/schema/v1/state.schema.json"
)

var ErrStateSchema = errors.New("persisted state schema is invalid")

type PersistedStateValidator struct {
	schema *jsonschema.Schema
	policy Policy
}

func NewPersistedStateValidator(
	rawModelsSchema []byte,
	rawStateSchema []byte,
	policy Policy,
) (PersistedStateValidator, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	for schemaURL, raw := range map[string][]byte{
		modelsSchemaURL: rawModelsSchema,
		stateSchemaURL:  rawStateSchema,
	} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			return PersistedStateValidator{}, fmt.Errorf("%w: decode %s: %w", ErrStateSchema, schemaURL, err)
		}
		if err := compiler.AddResource(schemaURL, document); err != nil {
			return PersistedStateValidator{}, fmt.Errorf("%w: register %s: %w", ErrStateSchema, schemaURL, err)
		}
	}
	schema, err := compiler.Compile(stateSchemaURL)
	if err != nil {
		return PersistedStateValidator{}, fmt.Errorf("%w: compile: %w", ErrStateSchema, err)
	}
	return PersistedStateValidator{schema: schema, policy: policy}, nil
}

func (v PersistedStateValidator) Validate(
	raw []byte,
	hostWorkspace string,
) (generated.PersistedJobState, Decision) {
	validated, decision := v.ValidateStored(raw, hostWorkspace)
	if !decision.Allowed {
		return generated.PersistedJobState{}, decision
	}
	if pathDecision := v.policy.validateWorkspaceDirectories(hostWorkspace, validated.Job.Cwd); !pathDecision.Allowed {
		return generated.PersistedJobState{}, corruptStateDecision()
	}
	return validated, decision
}

func (v PersistedStateValidator) ValidateStored(
	raw []byte,
	hostWorkspace string,
) (generated.PersistedJobState, Decision) {
	if !utf8.Valid(raw) {
		return generated.PersistedJobState{}, corruptStateDecision()
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil || v.schema.Validate(value) != nil {
		return generated.PersistedJobState{}, corruptStateDecision()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var state generated.PersistedJobState
	if err := decoder.Decode(&state); err != nil {
		return generated.PersistedJobState{}, corruptStateDecision()
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return generated.PersistedJobState{}, corruptStateDecision()
	}
	decision := v.policy.ValidatePersistedState(state)
	if !decision.Allowed {
		return generated.PersistedJobState{}, decision
	}
	if state.Job.WorkspacePath != hostWorkspace {
		return generated.PersistedJobState{}, corruptStateDecision()
	}
	return state, decision
}

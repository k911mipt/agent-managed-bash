package protocol

import (
	"bytes"
	"fmt"

	"github.com/k911mipt/agent-managed-bash/schemas"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaBaseURL = "https://agent-managed-bash.dev/schema/v1/"

type Validator struct {
	request  *jsonschema.Schema
	response *jsonschema.Schema
}

func NewValidator() (*Validator, error) {
	assets := schemas.V1()
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	resources := []struct {
		name string
		raw  []byte
	}{
		{name: "models.schema.json", raw: assets.ModelsSchema()},
		{name: "request.schema.json", raw: assets.RequestSchema()},
		{name: "response.schema.json", raw: assets.ResponseSchema()},
	}
	for _, resource := range resources {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
		if err != nil {
			return nil, fmt.Errorf("parse embedded %s: %w", resource.name, err)
		}
		if err := compiler.AddResource(schemaBaseURL+resource.name, document); err != nil {
			return nil, fmt.Errorf("add embedded %s: %w", resource.name, err)
		}
	}
	request, err := compiler.Compile(schemaBaseURL + "request.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile request schema: %w", err)
	}
	response, err := compiler.Compile(schemaBaseURL + "response.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile response schema: %w", err)
	}
	return &Validator{request: request, response: response}, nil
}

func (validator *Validator) ValidateRequest(raw []byte) error {
	return validateJSON(validator.request, raw)
}

func (validator *Validator) ValidateResponse(raw []byte) error {
	return validateJSON(validator.response, raw)
}

func validateJSON(schema *jsonschema.Schema, raw []byte) error {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("parse JSON document: %w", err)
	}
	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("validate JSON document: %w", err)
	}
	return nil
}

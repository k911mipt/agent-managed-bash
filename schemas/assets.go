package schemas

import (
	_ "embed"
	"slices"
)

var (
	//go:embed v1/models.schema.json
	modelsV1 []byte
	//go:embed v1/request.schema.json
	requestV1 []byte
	//go:embed v1/response.schema.json
	responseV1 []byte
	//go:embed v1/state.schema.json
	stateV1 []byte
	//go:embed v1/policy.schema.json
	policySchemaV1 []byte
	//go:embed v1/policy.json
	policyV1 []byte
)

type Assets struct{}

func V1() Assets {
	return Assets{}
}

func (Assets) ModelsSchema() []byte {
	return slices.Clone(modelsV1)
}

func (Assets) RequestSchema() []byte {
	return slices.Clone(requestV1)
}

func (Assets) ResponseSchema() []byte {
	return slices.Clone(responseV1)
}

func (Assets) StateSchema() []byte {
	return slices.Clone(stateV1)
}

func (Assets) PolicySchema() []byte {
	return slices.Clone(policySchemaV1)
}

func (Assets) Policy() []byte {
	return slices.Clone(policyV1)
}

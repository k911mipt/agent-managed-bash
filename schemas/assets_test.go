package schemas

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_V1_returns_immutable_embedded_contract_bytes(t *testing.T) {
	// Given
	assets := V1()
	models := assets.ModelsSchema()
	require.True(t, json.Valid(models))

	// When
	models[0] = 'x'

	// Then
	require.True(t, json.Valid(assets.ModelsSchema()))
	require.True(t, json.Valid(assets.StateSchema()))
	require.True(t, json.Valid(assets.PolicySchema()))
	require.True(t, json.Valid(assets.Policy()))
}

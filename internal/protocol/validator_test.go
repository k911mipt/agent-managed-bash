package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Validator_accepts_protocol_v1_request_and_response(t *testing.T) {
	// Given
	validator, err := NewValidator()
	require.NoError(t, err)
	request := []byte(`{"schema_version":1,"action":"version"}`)
	response := []byte(`{"schema_version":1,"ok":true,"action":"version","result":{"product":"managed-bash","binary_version":"dev","protocol_version":1,"os":"linux","architecture":"amd64"}}`)

	// When
	requestErr := validator.ValidateRequest(request)
	responseErr := validator.ValidateResponse(response)

	// Then
	require.NoError(t, requestErr)
	require.NoError(t, responseErr)
}

func Test_Validator_rejects_unknown_request_field(t *testing.T) {
	// Given
	validator, err := NewValidator()
	require.NoError(t, err)
	request := []byte(`{"schema_version":1,"action":"version","unexpected":true}`)

	// When
	validationErr := validator.ValidateRequest(request)

	// Then
	require.Error(t, validationErr)
}

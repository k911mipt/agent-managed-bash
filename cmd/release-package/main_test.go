package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_sourceDateEpoch_rejects_missing_and_invalid_values(t *testing.T) {
	tests := []string{"", "-1", "not-an-integer"}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			// Given
			t.Setenv("SOURCE_DATE_EPOCH", value)

			// When
			_, err := sourceDateEpoch()

			// Then
			require.Error(t, err)
		})
	}
}

func Test_sourceDateEpoch_accepts_non_negative_integer_seconds(t *testing.T) {
	// Given
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")

	// When
	epoch, err := sourceDateEpoch()

	// Then
	require.NoError(t, err)
	require.Equal(t, int64(1700000000), epoch.Unix())
}

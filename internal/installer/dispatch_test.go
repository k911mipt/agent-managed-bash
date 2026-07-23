//go:build linux || darwin

package installer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Dispatch_leaves_public_arguments_unhandled(t *testing.T) {
	// Given
	args := []string{"version"}

	// When
	handled, err := Dispatch(context.Background(), args, "0.1.0")

	// Then
	require.NoError(t, err)
	require.False(t, handled)
}

func Test_Dispatch_recognizes_malformed_hidden_install_without_falling_through(t *testing.T) {
	// Given
	args := []string{"--managed-bash-internal=install"}

	// When
	handled, err := Dispatch(context.Background(), args, "0.1.0")

	// Then
	require.True(t, handled)
	require.True(t, errors.Is(err, ErrInvalidArguments))
}

//go:build !linux && !darwin

package runner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_DispatchInternal_claims_guardian_mode_on_unsupported_platform(t *testing.T) {
	handled, err := DispatchInternal(context.Background(), []string{"--managed-bash-internal=guardian"})

	require.True(t, handled)
	require.ErrorIs(t, err, ErrUnsupported)
}

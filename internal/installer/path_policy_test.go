//go:build linux || darwin

package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Install_rejects_missing_config_home_below_shared_sticky_ancestor(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	shared := filepath.Join(physicalTempDir(t), "shared")
	require.NoError(t, os.Mkdir(shared, 0o700))
	require.NoError(t, os.Chmod(shared, 0o1777))
	layout.environment["XDG_CONFIG_HOME"] = filepath.Join(shared, "claimable-config")

	// When
	err := Install(context.Background(), layout.config(bundle, "0.1.0"))

	// Then
	require.ErrorIs(t, err, ErrUnsafePath)
	require.NoDirExists(t, layout.dataRoot)
}

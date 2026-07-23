//go:build linux || darwin

package installer

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Install_rejects_missing_config_home_below_shared_sticky_ancestor(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	shared := filepath.Join(physicalTempDir(t), "shared")
	require.NoError(t, os.Mkdir(shared, 0o700))
	require.NoError(t, os.Chmod(shared, 0o777|os.ModeSticky))
	layout.environment["XDG_CONFIG_HOME"] = filepath.Join(shared, "claimable-config")

	// When
	err := Install(context.Background(), layout.config(bundle, "0.1.0"))

	// Then
	require.ErrorIs(t, err, ErrUnsafePath)
	require.NoDirExists(t, layout.dataRoot)
}

func Test_Install_accepts_missing_config_home_below_owned_private_anchor(t *testing.T) {
	// Given
	layout := newTestLayout(t)
	bundle := writeTestBundle(t, "0.1.0", "first")
	private := physicalTempDir(t)
	layout.environment["XDG_CONFIG_HOME"] = filepath.Join(private, "config")

	// When
	err := Install(context.Background(), layout.config(bundle, "0.1.0"))

	// Then
	require.NoError(t, err)
}

func Test_ValidateRegistrationOwner_rejects_different_user(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "registration")
	require.NoError(t, os.Symlink("target", path))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	foreign := fileInfoWithSystem{FileInfo: info, system: &syscall.Stat_t{Uid: uint32(os.Geteuid() + 1)}}

	// When
	err = validateRegistrationOwner(foreign)

	// Then
	require.ErrorIs(t, err, ErrForeignPath)
}

type fileInfoWithSystem struct {
	os.FileInfo
	system any
}

func (info fileInfoWithSystem) Sys() any {
	return info.system
}

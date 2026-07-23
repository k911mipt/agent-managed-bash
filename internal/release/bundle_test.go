package release

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_VerifyBundle_accepts_exact_extracted_release(t *testing.T) {
	// Given
	root := writeTestBundle(t, "0.1.0")

	// When
	bundle, err := VerifyBundle(BundleConfig{
		Root: root, Version: "0.1.0", OS: runtime.GOOS, Architecture: runtime.GOARCH,
	})

	// Then
	require.NoError(t, err)
	require.Equal(t, "0.1.0", bundle.Version)
	require.Equal(t, runtime.GOOS, bundle.OS)
	require.Equal(t, runtime.GOARCH, bundle.Architecture)
	require.Len(t, bundle.Artifacts, len(artifactModes))
}

func Test_VerifyBundle_rejects_tampered_or_unsafe_extracted_release(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"tampered payload", func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("tampered"), 0o644))
		}},
		{"unknown entry", func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "unknown"), []byte("unknown"), 0o644))
		}},
		{"symlink", func(t *testing.T, root string) {
			path := filepath.Join(root, "README.md")
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Symlink("LICENSE", path))
		}},
		{"hard link", func(t *testing.T, root string) {
			path := filepath.Join(root, "README.md")
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Link(filepath.Join(root, "LICENSE"), path))
		}},
		{"wrong mode", func(t *testing.T, root string) {
			require.NoError(t, os.Chmod(filepath.Join(root, "README.md"), 0o600))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			root := writeTestBundle(t, "0.1.0")
			test.mutate(t, root)

			// When
			_, err := VerifyBundle(BundleConfig{
				Root: root, Version: "0.1.0", OS: runtime.GOOS, Architecture: runtime.GOARCH,
			})

			// Then
			require.ErrorIs(t, err, ErrInvalidArchive)
		})
	}
}

func Test_VerifyBundle_rejects_incompatible_identity(t *testing.T) {
	tests := []struct {
		name   string
		config BundleConfig
	}{
		{"version", BundleConfig{Version: "0.2.0", OS: runtime.GOOS, Architecture: runtime.GOARCH}},
		{"operating system", BundleConfig{Version: "0.1.0", OS: otherOS(), Architecture: runtime.GOARCH}},
		{"architecture", BundleConfig{Version: "0.1.0", OS: runtime.GOOS, Architecture: otherArchitecture()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			root := writeTestBundle(t, "0.1.0")
			test.config.Root = root

			// When
			_, err := VerifyBundle(test.config)

			// Then
			require.Error(t, err)
		})
	}
}

func writeTestBundle(t *testing.T, version string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "agent-managed-bash-"+version+"-"+runtime.GOOS+"-"+runtime.GOARCH)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "bin"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib", "opencode"), 0o755))
	payloads := validTestPayloads()
	for _, payload := range payloads {
		path := filepath.Join(root, filepath.FromSlash(payload.Path))
		require.NoError(t, os.WriteFile(path, payload.Data, os.FileMode(payload.Mode)))
	}
	expected := expectation{Version: version, Target: target{OS: runtime.GOOS, Architecture: runtime.GOARCH}}
	value, err := buildManifest(expected, payloads)
	require.NoError(t, err)
	raw, err := marshalManifest(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0o644))
	return root
}

func otherOS() string {
	if runtime.GOOS == "linux" {
		return "darwin"
	}
	return "linux"
}

func otherArchitecture() string {
	if runtime.GOARCH == "amd64" {
		return "arm64"
	}
	return "amd64"
}

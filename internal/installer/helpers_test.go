//go:build linux || darwin

package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

type testManifest struct {
	ManifestVersion int            `json:"manifest_version"`
	Product         string         `json:"product"`
	Version         string         `json:"version"`
	ProtocolVersion int            `json:"protocol_version"`
	Target          testTarget     `json:"target"`
	Artifacts       []testArtifact `json:"artifacts"`
}

type testTarget struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type testArtifact struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

type testBundleOptions struct {
	version          string
	marker           string
	binaryPathMarker string
}

type testLayout struct {
	home             string
	dataRoot         string
	binLink          string
	legacyPluginLink string
	environment      map[string]string
}

func newTestLayout(t *testing.T) testLayout {
	t.Helper()
	root := physicalTempDir(t)
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	home := filepath.Join(root, "home")
	dataHome := filepath.Join(root, "data")
	configHome := filepath.Join(root, "config")
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.Mkdir(home, 0o755))
	return testLayout{
		home: home, dataRoot: filepath.Join(dataHome, "agent-managed-bash"),
		binLink:          filepath.Join(binDir, "managed-bash"),
		legacyPluginLink: filepath.Join(configHome, "opencode", "plugins", "managed-bash.js"),
		environment: map[string]string{
			"HOME": home, "XDG_DATA_HOME": dataHome, "XDG_CONFIG_HOME": configHome, "MANAGED_BASH_BIN_DIR": binDir,
		},
	}
}

func (layout testLayout) config(bundleRoot string, version string) Config {
	return Config{BundleRoot: bundleRoot, BinaryVersion: version, LookupEnv: func(name string) (string, bool) {
		value, exists := layout.environment[name]
		return value, exists
	}}
}

func writeTestBundle(t *testing.T, version string, marker string) string {
	t.Helper()
	return writeTestBundleWithOptions(t, testBundleOptions{version: version, marker: marker})
}

func writeTestBundleWithOptions(t *testing.T, options testBundleOptions) string {
	t.Helper()
	root := filepath.Join(physicalTempDir(t), fmt.Sprintf("agent-managed-bash-%s-%s-%s", options.version, runtime.GOOS, runtime.GOARCH))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "bin"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib", "opencode"), 0o755))
	recordPath := ""
	if options.binaryPathMarker != "" {
		recordPath = fmt.Sprintf("if [ ! -e %q ]; then printf '%%s' \"$0\" >%q; fi\n", options.binaryPathMarker, options.binaryPathMarker)
	}
	binary := []byte(fmt.Sprintf(`#!/bin/sh
%sread request
printf '%%s\n' '{"schema_version":1,"action":"version","ok":true,"result":{"architecture":"%s","binary_version":"%s","os":"%s","product":"managed-bash","protocol_version":1}}'
`, recordPath, runtime.GOARCH, options.version, runtime.GOOS))
	files := map[string]struct {
		mode os.FileMode
		data []byte
	}{
		"LICENSE": {0o644, []byte("license")}, "README.md": {0o644, []byte("readme:" + options.marker)},
		"THIRD_PARTY_NOTICES.txt": {0o644, []byte("notices")}, "bin/managed-bash": {0o755, binary},
		"install.sh": {0o755, []byte("#!/bin/sh\n")}, "lib/opencode/managed-bash.js": {0o644, []byte("plugin:" + options.marker)},
		"uninstall.sh": {0o755, []byte("#!/bin/sh\n")},
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	artifacts := make([]testArtifact, 0, len(paths))
	for _, path := range paths {
		file := files[path]
		digest := sha256.Sum256(file.data)
		artifacts = append(artifacts, testArtifact{Path: path, Mode: fmt.Sprintf("%04o", file.mode), SHA256: hex.EncodeToString(digest[:])})
		require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), file.data, file.mode))
	}
	manifest := testManifest{
		ManifestVersion: 1, Product: "managed-bash", Version: options.version, ProtocolVersion: 1,
		Target: testTarget{OS: runtime.GOOS, Architecture: runtime.GOARCH}, Artifacts: artifacts,
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "manifest.json"), append(raw, '\n'), 0o644))
	return root
}

func physicalTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return root
}

func currentTarget(t *testing.T, dataRoot string) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(dataRoot, "current"))
	require.NoError(t, err)
	return target
}

func requireOwnedBinaryLink(t *testing.T, layout testLayout) {
	t.Helper()
	binTarget, err := os.Readlink(layout.binLink)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(layout.dataRoot, "current", "bin", "managed-bash"), binTarget)
}

func writeOwnedLegacyPluginLink(t *testing.T, layout testLayout) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(layout.legacyPluginLink), 0o755))
	require.NoError(t, os.Symlink(
		filepath.Join(layout.dataRoot, "current", "lib", "opencode", "managed-bash.js"),
		layout.legacyPluginLink,
	))
}

func requireOwnedLegacyPluginLink(t *testing.T, layout testLayout) {
	t.Helper()
	target, err := os.Readlink(layout.legacyPluginLink)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(layout.dataRoot, "current", "lib", "opencode", "managed-bash.js"), target)
}

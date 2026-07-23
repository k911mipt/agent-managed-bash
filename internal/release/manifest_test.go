package release

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

func Test_parseManifest_accepts_exact_release_contract(t *testing.T) {
	// Given
	expected := testExpectation("linux", "amd64")
	raw := marshalTestManifest(t, validTestManifest())

	// When
	parsed, err := parseManifest(raw, expected)

	// Then
	require.NoError(t, err)
	require.Equal(t, "0.1.0", parsed.Version)
	require.Equal(t, "linux", parsed.Target.OS)
	require.Len(t, parsed.Artifacts, 7)
}

func Test_parseManifest_rejects_unknown_fields(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "top-level field",
			raw:  strings.Replace(string(marshalTestManifest(t, validTestManifest())), `"product":`, `"unknown":true,"product":`, 1),
		},
		{
			name: "target field",
			raw:  strings.Replace(string(marshalTestManifest(t, validTestManifest())), `"architecture":`, `"unknown":true,"architecture":`, 1),
		},
		{
			name: "artifact field",
			raw:  strings.Replace(string(marshalTestManifest(t, validTestManifest())), `"mode":`, `"unknown":true,"mode":`, 1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			_, err := parseManifest([]byte(test.raw), testExpectation("linux", "amd64"))

			// Then
			require.Error(t, err)
		})
	}
}

func Test_parseManifest_rejects_invalid_identity_and_artifacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testManifest)
	}{
		{"manifest version", func(value *testManifest) { value.ManifestVersion = 2 }},
		{"product", func(value *testManifest) { value.Product = "other" }},
		{"release version", func(value *testManifest) { value.Version = "0.2.0" }},
		{"protocol version", func(value *testManifest) { value.ProtocolVersion = 2 }},
		{"target os", func(value *testManifest) { value.Target.OS = "darwin" }},
		{"target architecture", func(value *testManifest) { value.Target.Architecture = "arm64" }},
		{"duplicate path", func(value *testManifest) { value.Artifacts[1].Path = value.Artifacts[0].Path }},
		{"absolute path", func(value *testManifest) { value.Artifacts[0].Path = "/bin/managed-bash" }},
		{"traversal path", func(value *testManifest) { value.Artifacts[0].Path = "../managed-bash" }},
		{"backslash path", func(value *testManifest) { value.Artifacts[0].Path = `bin\managed-bash` }},
		{"wrong mode", func(value *testManifest) { value.Artifacts[0].Mode = "0777" }},
		{"uppercase hash", func(value *testManifest) { value.Artifacts[0].SHA256 = strings.Repeat("A", 64) }},
		{"short hash", func(value *testManifest) { value.Artifacts[0].SHA256 = "abc" }},
		{"unsorted paths", func(value *testManifest) {
			value.Artifacts[0], value.Artifacts[1] = value.Artifacts[1], value.Artifacts[0]
		}},
		{"undeclared path", func(value *testManifest) { value.Artifacts[0].Path = "extra" }},
		{"missing path", func(value *testManifest) { value.Artifacts = value.Artifacts[1:] }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			manifest := validTestManifest()
			test.mutate(&manifest)

			// When
			_, err := parseManifest(marshalTestManifest(t, manifest), testExpectation("linux", "amd64"))

			// Then
			require.Error(t, err)
		})
	}
}

func Test_parseManifest_rejects_trailing_json(t *testing.T) {
	// Given
	raw := append(marshalTestManifest(t, validTestManifest()), []byte(` {}`)...)

	// When
	_, err := parseManifest(raw, testExpectation("linux", "amd64"))

	// Then
	require.Error(t, err)
}

func validTestManifest() testManifest {
	paths := requiredArtifactPaths()
	artifacts := make([]testArtifact, len(paths))
	for index, artifactPath := range paths {
		artifacts[index] = testArtifact{Path: artifactPath, Mode: requiredArtifactMode(artifactPath), SHA256: strings.Repeat("a", 64)}
	}
	return testManifest{
		ManifestVersion: 1,
		Product:         "managed-bash",
		Version:         "0.1.0",
		ProtocolVersion: 1,
		Target:          testTarget{OS: "linux", Architecture: "amd64"},
		Artifacts:       artifacts,
	}
}

func testExpectation(goos string, goarch string) expectation {
	return expectation{Version: "0.1.0", Target: target{OS: goos, Architecture: goarch}, Epoch: time.Unix(1_700_000_000, 0).UTC()}
}

func marshalTestManifest(t *testing.T, manifest testManifest) []byte {
	t.Helper()
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	return raw
}

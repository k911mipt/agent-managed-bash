package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

type manifest struct {
	ManifestVersion int        `json:"manifest_version"`
	Product         string     `json:"product"`
	Version         string     `json:"version"`
	ProtocolVersion int        `json:"protocol_version"`
	Target          target     `json:"target"`
	Artifacts       []artifact `json:"artifacts"`
}

type artifact struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

func parseManifest(raw []byte, expected expectation) (manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed manifest
	if err := decoder.Decode(&parsed); err != nil {
		return manifest{}, fmt.Errorf("decode: %w: %w", err, ErrInvalidManifest)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return manifest{}, fmt.Errorf("trailing JSON: %w", ErrInvalidManifest)
	}
	if err := validateManifest(parsed, expected); err != nil {
		return manifest{}, err
	}
	return parsed, nil
}

func buildManifest(expected expectation, payloads []payloadFile) (manifest, error) {
	byPath := make(map[string]payloadFile, len(payloads))
	for _, payload := range payloads {
		if _, exists := byPath[payload.Path]; exists {
			return manifest{}, fmt.Errorf("duplicate payload %q: %w", payload.Path, ErrInvalidManifest)
		}
		byPath[payload.Path] = payload
	}
	artifacts := make([]artifact, 0, len(artifactModes))
	for _, artifactPath := range requiredArtifactPaths() {
		payload, exists := byPath[artifactPath]
		if !exists || payload.Mode != artifactModes[artifactPath] {
			return manifest{}, fmt.Errorf("payload %q missing or has wrong mode: %w", artifactPath, ErrInvalidManifest)
		}
		digest := sha256.Sum256(payload.Data)
		artifacts = append(artifacts, artifact{
			Path: artifactPath, Mode: requiredArtifactMode(artifactPath), SHA256: hex.EncodeToString(digest[:]),
		})
	}
	if len(byPath) != len(artifactModes) {
		return manifest{}, fmt.Errorf("payload contains undeclared files: %w", ErrInvalidManifest)
	}
	return manifest{
		ManifestVersion: manifestVersion,
		Product:         product,
		Version:         expected.Version,
		ProtocolVersion: protocolVersion,
		Target:          expected.Target,
		Artifacts:       artifacts,
	}, nil
}

func marshalManifest(value manifest) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return append(raw, '\n'), nil
}

func validateManifest(value manifest, expected expectation) error {
	if value.ManifestVersion != manifestVersion || value.Product != product || value.Version != expected.Version ||
		value.ProtocolVersion != protocolVersion || value.Target != expected.Target {
		return fmt.Errorf("release identity mismatch: %w", ErrInvalidManifest)
	}
	expectedPaths := requiredArtifactPaths()
	if len(value.Artifacts) != len(expectedPaths) {
		return fmt.Errorf("artifact count %d: %w", len(value.Artifacts), ErrInvalidManifest)
	}
	seen := make(map[string]struct{}, len(value.Artifacts))
	for index, item := range value.Artifacts {
		if err := validateArtifact(item, expectedPaths[index]); err != nil {
			return err
		}
		if _, exists := seen[item.Path]; exists {
			return fmt.Errorf("duplicate artifact %q: %w", item.Path, ErrInvalidManifest)
		}
		seen[item.Path] = struct{}{}
	}
	return nil
}

func validateArtifact(item artifact, expectedPath string) error {
	if item.Path != expectedPath || path.IsAbs(item.Path) || path.Clean(item.Path) != item.Path ||
		strings.Contains(item.Path, `\`) {
		return fmt.Errorf("artifact path %q: %w", item.Path, ErrInvalidManifest)
	}
	if item.Mode != requiredArtifactMode(item.Path) {
		return fmt.Errorf("artifact mode %q for %q: %w", item.Mode, item.Path, ErrInvalidManifest)
	}
	if len(item.SHA256) != sha256.Size*2 || strings.ToLower(item.SHA256) != item.SHA256 {
		return fmt.Errorf("artifact hash for %q: %w", item.Path, ErrInvalidManifest)
	}
	if _, err := hex.DecodeString(item.SHA256); err != nil {
		return fmt.Errorf("artifact hash for %q: %w: %w", item.Path, err, ErrInvalidManifest)
	}
	return nil
}

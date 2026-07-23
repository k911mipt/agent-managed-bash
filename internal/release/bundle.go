//go:build linux || darwin

package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type BundleConfig struct {
	Root         string
	Version      string
	OS           string
	Architecture string
}

type Bundle struct {
	Root         string
	Version      string
	OS           string
	Architecture string
	Manifest     []byte
	Artifacts    []BundleArtifact
}

type BundleArtifact struct {
	Path   string
	Mode   os.FileMode
	SHA256 string
}

type bundleEntry struct {
	directory bool
	mode      os.FileMode
}

func VerifyBundle(config BundleConfig) (Bundle, error) {
	expected, err := newExpectation(
		config.Version,
		target{OS: config.OS, Architecture: config.Architecture},
		time.Unix(1, 0),
	)
	if err != nil {
		return Bundle{}, err
	}
	root, err := physicalBundleRoot(config.Root, expected)
	if err != nil {
		return Bundle{}, err
	}
	manifestData, err := readBundleFile(filepath.Join(root, "manifest.json"), 0o644)
	if err != nil {
		return Bundle{}, err
	}
	parsed, err := parseManifest(manifestData, expected)
	if err != nil {
		return Bundle{}, err
	}
	canonical, err := marshalManifest(parsed)
	if err != nil {
		return Bundle{}, err
	}
	if !bytes.Equal(manifestData, canonical) {
		return Bundle{}, fmt.Errorf("noncanonical manifest encoding: %w", ErrInvalidArchive)
	}
	if err := verifyBundleTree(root, parsed); err != nil {
		return Bundle{}, err
	}
	artifacts := make([]BundleArtifact, len(parsed.Artifacts))
	for index, item := range parsed.Artifacts {
		artifacts[index] = BundleArtifact{
			Path: item.Path, Mode: os.FileMode(requiredArtifactModeValue(item.Path)), SHA256: item.SHA256,
		}
	}
	return Bundle{
		Root: root, Version: parsed.Version, OS: parsed.Target.OS, Architecture: parsed.Target.Architecture,
		Manifest: manifestData, Artifacts: artifacts,
	}, nil
}

func physicalBundleRoot(root string, expected expectation) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Base(root) != archiveRoot(expected) {
		return "", fmt.Errorf("bundle root %q: %w", root, ErrInvalidArchive)
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve bundle root: %w: %w", err, ErrInvalidArchive)
	}
	if physical != root {
		return "", fmt.Errorf("bundle root contains a symlink: %w", ErrInvalidArchive)
	}
	return root, nil
}

func verifyBundleTree(root string, parsed manifest) error {
	expected := map[string]bundleEntry{
		".": {directory: true, mode: 0o755}, "bin": {directory: true, mode: 0o755},
		"lib": {directory: true, mode: 0o755}, "lib/opencode": {directory: true, mode: 0o755},
		"manifest.json": {mode: 0o644},
	}
	hashes := make(map[string]string, len(parsed.Artifacts))
	for _, item := range parsed.Artifacts {
		expected[filepath.FromSlash(item.Path)] = bundleEntry{mode: os.FileMode(requiredArtifactModeValue(item.Path))}
		hashes[filepath.FromSlash(item.Path)] = item.SHA256
	}
	seen := make(map[string]struct{}, len(expected))
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk bundle: %w", walkErr)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativize bundle entry: %w", err)
		}
		required, exists := expected[relative]
		if !exists {
			return fmt.Errorf("unknown bundle entry %q: %w", relative, ErrInvalidArchive)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect bundle entry %q: %w", relative, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != required.mode ||
			info.IsDir() != required.directory || (!required.directory && !info.Mode().IsRegular()) {
			return fmt.Errorf("invalid bundle entry %q: %w", relative, ErrInvalidArchive)
		}
		seen[relative] = struct{}{}
		if required.directory || relative == "manifest.json" {
			return nil
		}
		data, err := readBundleFile(path, required.mode)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != hashes[relative] {
			return fmt.Errorf("hash mismatch for %q: %w", relative, ErrInvalidArchive)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("bundle layout incomplete: %w", ErrInvalidArchive)
	}
	return nil
}

func readBundleFile(path string, mode os.FileMode) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect bundle file %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open bundle file %q: %w", path, err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect open bundle file %q: %w", path, err)
	}
	stat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || !after.Mode().IsRegular() || !os.SameFile(before, after) ||
		after.Mode().Perm() != mode || stat.Nlink != 1 {
		return nil, fmt.Errorf("unsafe bundle file %q: %w", path, ErrInvalidArchive)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read bundle file %q: %w", path, err)
	}
	return data, nil
}

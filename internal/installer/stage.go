//go:build linux || darwin

package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/k911mipt/agent-managed-bash/internal/release"
)

var installedArtifactPaths = []string{
	"LICENSE", "README.md", "THIRD_PARTY_NOTICES.txt", "bin/managed-bash", "lib/opencode/managed-bash.js",
}

type installedEntry struct {
	directory bool
	mode      os.FileMode
}

func prepareRelease(paths installPaths, bundle release.Bundle, beforeRename func(string)) (string, bool, error) {
	if err := ensureDirectory(paths.releases, 0o700); err != nil {
		return "", false, err
	}
	if err := validateOwnedDirectory(paths.releases, 0o700); err != nil {
		return "", false, err
	}
	final := filepath.Join(paths.releases, releaseName(identityFromBundle(bundle)))
	if _, err := os.Lstat(final); err == nil {
		if err := validateInstalledRelease(final, bundle); err != nil {
			return "", false, err
		}
		return final, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, fmt.Errorf("inspect release destination: %w", err)
	}
	stage, err := os.MkdirTemp(paths.releases, ".staging-")
	if err != nil {
		return "", false, fmt.Errorf("create release stage: %w", err)
	}
	if err := populateRelease(stage, bundle); err != nil {
		return "", false, errors.Join(err, removeRelease(stage))
	}
	if beforeRename != nil {
		beforeRename(final)
	}
	if err := renameNoReplace(stage, final); errors.Is(err, os.ErrExist) {
		return "", false, errors.Join(fmt.Errorf("release destination appeared during publication: %w", ErrForeignPath), removeRelease(stage))
	} else if err != nil {
		return "", false, errors.Join(fmt.Errorf("publish release: %w", err), removeRelease(stage))
	}
	if err := syncDirectory(paths.releases); err != nil {
		return "", false, errors.Join(err, removeRelease(final))
	}
	return final, true, nil
}

func populateRelease(stage string, bundle release.Bundle) error {
	for _, directory := range []string{"bin", "lib", filepath.Join("lib", "opencode")} {
		if err := os.Mkdir(filepath.Join(stage, directory), 0o700); err != nil {
			return fmt.Errorf("create staged directory %q: %w", directory, err)
		}
	}
	artifacts := make(map[string]release.BundleArtifact, len(bundle.Artifacts))
	for _, artifact := range bundle.Artifacts {
		artifacts[artifact.Path] = artifact
	}
	for _, path := range installedArtifactPaths {
		artifact := artifacts[path]
		if err := copyStagedFile(
			filepath.Join(bundle.Root, filepath.FromSlash(path)), filepath.Join(stage, filepath.FromSlash(path)), artifact,
		); err != nil {
			return err
		}
	}
	if err := writeStagedFile(filepath.Join(stage, "manifest.json"), bundle.Manifest, 0o444); err != nil {
		return err
	}
	for _, directory := range []string{filepath.Join(stage, "lib", "opencode"), filepath.Join(stage, "bin"), filepath.Join(stage, "lib"), stage} {
		if err := syncDirectory(directory); err != nil {
			return err
		}
		if err := os.Chmod(directory, 0o555); err != nil {
			return fmt.Errorf("make release directory immutable: %w", err)
		}
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func copyStagedFile(source string, destination string, artifact release.BundleArtifact) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open bundle artifact %q: %w", artifact.Path, err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create staged artifact %q: %w", artifact.Path, err)
	}
	digest := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(output, digest), input)
	if copyErr != nil || hex.EncodeToString(digest.Sum(nil)) != artifact.SHA256 {
		return errors.Join(fmt.Errorf("copy staged artifact %q: %w", artifact.Path, errors.Join(copyErr, release.ErrInvalidArchive)), output.Close())
	}
	mode := os.FileMode(0o444)
	if artifact.Path == "bin/managed-bash" {
		mode = 0o555
	}
	if err := output.Chmod(mode); err != nil {
		return errors.Join(fmt.Errorf("set staged artifact mode: %w", err), output.Close())
	}
	if err := output.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync staged artifact: %w", err), output.Close())
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close staged artifact: %w", err)
	}
	return nil
}

func writeStagedFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create staged manifest: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write staged manifest: %w", err), file.Close())
	}
	if err := file.Chmod(mode); err != nil {
		return errors.Join(fmt.Errorf("set staged manifest mode: %w", err), file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync staged manifest: %w", err), file.Close())
	}
	return file.Close()
}

func validateInstalledRelease(root string, bundle release.Bundle) error {
	hashes := make(map[string]string, len(bundle.Artifacts))
	for _, artifact := range bundle.Artifacts {
		hashes[filepath.FromSlash(artifact.Path)] = artifact.SHA256
	}
	return validateInstalledTree(root, func(path string, data []byte) error {
		if path == "manifest.json" {
			if string(data) != string(bundle.Manifest) {
				return ErrForeignPath
			}
			return nil
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != hashes[path] {
			return ErrForeignPath
		}
		return nil
	})
}

func validateInstalledTree(root string, validate func(string, []byte) error) error {
	expected := map[string]installedEntry{
		".": {directory: true, mode: 0o555}, "bin": {directory: true, mode: 0o555},
		"lib": {directory: true, mode: 0o555}, "lib/opencode": {directory: true, mode: 0o555},
		"manifest.json": {mode: 0o444},
	}
	for _, path := range installedArtifactPaths {
		expected[filepath.FromSlash(path)] = installedEntry{mode: 0o444}
	}
	expected[filepath.FromSlash("bin/managed-bash")] = installedEntry{mode: 0o555}
	seen := make([]string, 0, len(expected))
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		required, exists := expected[relative]
		if !exists {
			return ErrForeignPath
		}
		info, err := entry.Info()
		if err != nil {
			return errors.Join(err, ErrForeignPath)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || info.Mode().Perm() != required.mode || info.Mode()&os.ModeSymlink != 0 ||
			info.IsDir() != required.directory || stat.Uid != uint32(os.Geteuid()) || (!required.directory && stat.Nlink != 1) {
			return errors.Join(err, ErrForeignPath)
		}
		seen = append(seen, relative)
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return ErrForeignPath
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if validate != nil {
			return validate(relative, data)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("validate installed release %q: %w", root, err)
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("installed release layout: %w", ErrForeignPath)
	}
	return nil
}

func removeRelease(root string) error {
	for _, directory := range []string{root, filepath.Join(root, "bin"), filepath.Join(root, "lib"), filepath.Join(root, "lib", "opencode")} {
		if err := os.Chmod(directory, 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("make release removable: %w", err)
		}
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove release: %w", err)
	}
	return nil
}

package release

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type BuildConfig struct {
	RepositoryRoot  string
	OutputDirectory string
	Epoch           time.Time
}

type VerifyConfig struct {
	ArchivePath  string
	Version      string
	OS           string
	Architecture string
	Epoch        time.Time
}

type binaryBuild struct {
	RepositoryRoot string
	OutputPath     string
	Expected       expectation
}

func Build(ctx context.Context, config BuildConfig) ([]string, error) {
	version, err := readVersion(filepath.Join(config.RepositoryRoot, "VERSION"))
	if err != nil {
		return nil, err
	}
	if config.Epoch.IsZero() || config.Epoch.Nanosecond() != 0 {
		return nil, fmt.Errorf("SOURCE_DATE_EPOCH is required and must be whole seconds: %w", ErrInvalidArchive)
	}
	if err := os.MkdirAll(config.OutputDirectory, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	stage, err := os.MkdirTemp(config.OutputDirectory, ".release-stage-")
	if err != nil {
		return nil, fmt.Errorf("create release stage: %w", err)
	}
	defer os.RemoveAll(stage)

	shared, err := readSharedPayloads(ctx, config.RepositoryRoot, version)
	if err != nil {
		return nil, err
	}
	archives := make([]string, 0, len(targetMatrix))
	for _, releaseTarget := range targetMatrix {
		expected, err := newExpectation(version, releaseTarget, config.Epoch)
		if err != nil {
			return nil, err
		}
		binaryPath := filepath.Join(stage, releaseTarget.OS+"-"+releaseTarget.Architecture, "managed-bash")
		if err := buildBinary(ctx, binaryBuild{RepositoryRoot: config.RepositoryRoot, OutputPath: binaryPath, Expected: expected}); err != nil {
			return nil, err
		}
		binary, err := readRegularFile(binaryPath)
		if err != nil {
			return nil, err
		}
		payloads := append([]payloadFile{{Path: "bin/managed-bash", Mode: 0o755, Data: binary}}, shared...)
		archivePath := filepath.Join(config.OutputDirectory, archiveRoot(expected)+".tar.gz")
		if err := buildArchiveFile(archivePath, expected, payloads); err != nil {
			return nil, err
		}
		archives = append(archives, archivePath)
	}
	return archives, nil
}

func VerifyFile(config VerifyConfig) error {
	releaseTarget := target{OS: config.OS, Architecture: config.Architecture}
	expected, err := newExpectation(config.Version, releaseTarget, config.Epoch)
	if err != nil {
		return err
	}
	archive, err := os.Open(config.ArchivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer archive.Close()
	return verifyArchive(archive, expected)
}

func buildBinary(ctx context.Context, build binaryBuild) error {
	if err := os.MkdirAll(filepath.Dir(build.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create binary stage: %w", err)
	}
	command := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false",
		"-ldflags=-X main.binaryVersion="+build.Expected.Version, "-o", build.OutputPath, "./cmd/managed-bash")
	command.Dir = build.RepositoryRoot
	command.Env = append(os.Environ(),
		"CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOOS="+build.Expected.Target.OS, "GOARCH="+build.Expected.Target.Architecture,
	)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build %s/%s: %w: %s", build.Expected.Target.OS, build.Expected.Target.Architecture, err, stderr.String())
	}
	return nil
}

func buildArchiveFile(archivePath string, expected expectation, payloads []payloadFile) (err error) {
	temporaryPath := archivePath + ".tmp"
	defer func() {
		if err != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	output, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	if err := writeArchive(output, expected, payloads); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close archive: %w", err)
	}
	if err := VerifyFile(VerifyConfig{
		ArchivePath: temporaryPath, Version: expected.Version, OS: expected.Target.OS,
		Architecture: expected.Target.Architecture, Epoch: expected.Epoch,
	}); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, archivePath); err != nil {
		return fmt.Errorf("publish archive: %w", err)
	}
	return nil
}

func readSharedPayloads(ctx context.Context, root string, version string) ([]payloadFile, error) {
	sources := []struct {
		archivePath string
		sourcePath  string
	}{
		{"LICENSE", "LICENSE"},
		{"README.md", "README.md"},
		{"THIRD_PARTY_NOTICES.txt", "packaging/THIRD_PARTY_NOTICES.txt"},
		{"install.sh", "packaging/install.sh"},
		{"lib/opencode/managed-bash.js", "plugins/opencode/dist/managed-bash.js"},
		{"uninstall.sh", "packaging/uninstall.sh"},
	}
	payloads := make([]payloadFile, 0, len(sources))
	for _, source := range sources {
		sourcePath := filepath.Join(root, source.sourcePath)
		data, err := readRegularFile(sourcePath)
		if err != nil {
			return nil, err
		}
		if source.archivePath == "lib/opencode/managed-bash.js" {
			if err := verifyPluginRelease(ctx, sourcePath, version); err != nil {
				return nil, err
			}
		}
		payloads = append(payloads, payloadFile{Path: source.archivePath, Mode: artifactModes[source.archivePath], Data: data})
	}
	return payloads, nil
}

func verifyPluginRelease(ctx context.Context, pluginPath string, expected string) error {
	command := exec.CommandContext(ctx, "bun", "-e", `
const bundle = await import(process.env.MANAGED_BASH_PLUGIN_BUNDLE)
const version = bundle.ManagedBashPlugin?.managedBashReleaseVersion
if (typeof version !== "string") process.exit(2)
process.stdout.write(version)
`)
	command.Env = append(os.Environ(), "MANAGED_BASH_PLUGIN_BUNDLE="+pluginPath)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect plugin release: %w: %s", err, output)
	}
	if string(output) != expected {
		return fmt.Errorf("plugin release %q does not match VERSION %q: %w", output, expected, ErrInvalidArchive)
	}
	return nil
}

func readRegularFile(filePath string) ([]byte, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", filePath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source %s is not a regular file", filePath)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filePath, err)
	}
	return data, nil
}

func readVersion(versionPath string) (string, error) {
	raw, err := readRegularFile(versionPath)
	if err != nil {
		return "", err
	}
	version := strings.TrimSuffix(string(raw), "\n")
	if string(raw) != version+"\n" || !versionPattern.MatchString(version) {
		return "", fmt.Errorf("VERSION must contain one semantic version and a newline: %w", ErrInvalidManifest)
	}
	return version, nil
}

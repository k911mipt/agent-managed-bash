//go:build linux || darwin

package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/k911mipt/agent-managed-bash/internal/release"
)

var (
	ErrForeignPath      = errors.New("installer path is not owned by managed-bash")
	ErrUnsafePath       = errors.New("unsafe installer path")
	ErrInvalidArguments = errors.New("invalid internal installer arguments")
)

type Config struct {
	BundleRoot    string
	BinaryVersion string
	LookupEnv     func(string) (string, bool)
}

type identity struct {
	version      string
	os           string
	architecture string
}

type installPaths struct {
	dataHome   string
	dataRoot   string
	releases   string
	current    string
	lock       string
	binLink    string
	pluginLink string
}

type currentPointer struct {
	target string
	exists bool
}

type hooks struct {
	beforeLock          func()
	afterLock           func()
	afterLockOpen       func(int)
	beforeLockUnlink    func()
	beforeCommit        func() error
	beforePluginLink    func() error
	beforeLinkRename    func(string)
	afterLinkRename     func(string) error
	beforeLinkCleanup   func(string) error
	beforeReleaseRename func(string)
	afterCurrentRename  func() error
	verifyInstalled     func(context.Context, string, identity) error
}

func pathsFromConfig(config Config) (installPaths, error) {
	lookup := config.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	home, exists := lookup("HOME")
	if !exists || home == "" {
		return installPaths{}, fmt.Errorf("HOME is required: %w", ErrUnsafePath)
	}
	if err := validatePathComponents(home); err != nil {
		return installPaths{}, fmt.Errorf("HOME: %w", err)
	}
	dataHome := environmentPath(lookup, "XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	configHome := environmentPath(lookup, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	binDir := environmentPath(lookup, "MANAGED_BASH_BIN_DIR", filepath.Join(home, ".local", "bin"))
	for name, path := range map[string]string{"data home": dataHome, "config home": configHome, "binary directory": binDir} {
		if err := validatePathComponents(path); err != nil {
			return installPaths{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	dataRoot := filepath.Join(dataHome, "agent-managed-bash")
	paths := installPaths{
		dataHome: dataHome, dataRoot: dataRoot, releases: filepath.Join(dataRoot, "releases"),
		current: filepath.Join(dataRoot, "current"), lock: filepath.Join(dataHome, ".agent-managed-bash.install.lock"),
		binLink:    filepath.Join(binDir, "managed-bash"),
		pluginLink: filepath.Join(configHome, "opencode", "plugins", "managed-bash.js"),
	}
	for name, path := range map[string]string{"binary registration": paths.binLink, "plugin registration": paths.pluginLink} {
		if pathInside(dataRoot, path) {
			return installPaths{}, fmt.Errorf("%s is inside installation data root: %w", name, ErrUnsafePath)
		}
	}
	return paths, nil
}

func pathInside(parent string, path string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func environmentPath(lookup func(string) (string, bool), name string, fallback string) string {
	if value, exists := lookup(name); exists && value != "" {
		return value
	}
	return fallback
}

func identityFromBundle(bundle release.Bundle) identity {
	return identity{version: bundle.Version, os: bundle.OS, architecture: bundle.Architecture}
}

func hostTarget() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func releaseName(value identity) string {
	return value.version + "-" + value.os + "-" + value.architecture
}

func currentReleaseTarget(value identity) string {
	return filepath.ToSlash(filepath.Join("releases", releaseName(value)))
}

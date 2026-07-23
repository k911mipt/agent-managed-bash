package release

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"
)

const (
	manifestVersion = 1
	product         = "managed-bash"
	protocolVersion = 1
)

var (
	ErrInvalidManifest = errors.New("invalid release manifest")
	ErrInvalidArchive  = errors.New("invalid release archive")
	versionPattern     = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	targetMatrix       = []target{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
		{OS: "darwin", Architecture: "amd64"},
		{OS: "darwin", Architecture: "arm64"},
	}
	artifactModes = map[string]int64{
		"LICENSE":                      0o644,
		"README.md":                    0o644,
		"THIRD_PARTY_NOTICES.txt":      0o644,
		"bin/managed-bash":             0o755,
		"install.sh":                   0o755,
		"lib/opencode/managed-bash.js": 0o644,
		"uninstall.sh":                 0o755,
	}
)

type target struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type expectation struct {
	Version string
	Target  target
	Epoch   time.Time
}

type payloadFile struct {
	Path string
	Mode int64
	Data []byte
}

func newExpectation(version string, releaseTarget target, epoch time.Time) (expectation, error) {
	if !versionPattern.MatchString(version) {
		return expectation{}, fmt.Errorf("version %q: %w", version, ErrInvalidManifest)
	}
	if !knownTarget(releaseTarget) {
		return expectation{}, fmt.Errorf("target %s/%s: %w", releaseTarget.OS, releaseTarget.Architecture, ErrInvalidManifest)
	}
	if epoch.IsZero() || epoch.Nanosecond() != 0 {
		return expectation{}, fmt.Errorf("source date epoch must be whole seconds: %w", ErrInvalidArchive)
	}
	return expectation{Version: version, Target: releaseTarget, Epoch: epoch.UTC()}, nil
}

func knownTarget(candidate target) bool {
	for _, supported := range targetMatrix {
		if candidate == supported {
			return true
		}
	}
	return false
}

func requiredArtifactPaths() []string {
	paths := make([]string, 0, len(artifactModes))
	for artifactPath := range artifactModes {
		paths = append(paths, artifactPath)
	}
	sort.Strings(paths)
	return paths
}

func requiredArtifactMode(artifactPath string) string {
	return fmt.Sprintf("%04o", artifactModes[artifactPath])
}

func requiredArtifactModeValue(artifactPath string) int64 {
	return artifactModes[artifactPath]
}

func archiveRoot(expected expectation) string {
	return fmt.Sprintf("agent-managed-bash-%s-%s-%s", expected.Version, expected.Target.OS, expected.Target.Architecture)
}

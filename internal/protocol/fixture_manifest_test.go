package protocol_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/k911mipt/agent-managed-bash/internal/protocol/generated"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

var errInvalidUTF8 = errors.New("fixture is not valid UTF-8")

type fixtureManifest struct {
	Cases []fixtureCase `json:"cases"`
}

func Test_FixtureManifest_references_every_fixture_exactly_once(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	fixtureRoot := filepath.Join(repositoryRoot, "fixtures", "v1", "schema")
	manifest := loadFixtureManifest(t, repositoryRoot)
	names := make(map[string]struct{}, len(manifest.Cases))
	paths := make(map[string]struct{}, len(manifest.Cases))
	for _, fixture := range manifest.Cases {
		_, duplicateName := names[fixture.Name]
		require.False(t, duplicateName, "duplicate fixture name %q", fixture.Name)
		names[fixture.Name] = struct{}{}
		_, duplicatePath := paths[fixture.Path]
		require.False(t, duplicatePath, "duplicate fixture path %q", fixture.Path)
		paths[fixture.Path] = struct{}{}
		require.True(t, filepath.IsLocal(fixture.Path), "non-local fixture path %q", fixture.Path)
		info, err := os.Stat(filepath.Join(fixtureRoot, fixture.Path))
		require.NoError(t, err)
		require.True(t, info.Mode().IsRegular())
	}

	diskPaths := make(map[string]struct{}, len(paths))
	require.NoError(t, filepath.WalkDir(fixtureRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() == "manifest.json" {
			return nil
		}
		relative, err := filepath.Rel(fixtureRoot, path)
		if err != nil {
			return err
		}
		diskPaths[relative] = struct{}{}
		return nil
	}))
	require.Equal(t, diskPaths, paths)
}

func Test_FixtureManifest_covers_every_contract_variant(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	manifest := loadFixtureManifest(t, repositoryRoot)
	requestActions := make(map[generated.Action]struct{})
	responseActions := make(map[generated.Action]struct{})
	errorCodes := make(map[generated.ErrorCode]struct{})
	stateStatuses := make(map[generated.JobStatus]struct{})
	for _, fixture := range manifest.Cases {
		if !fixture.Valid {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(repositoryRoot, "fixtures", "v1", "schema", fixture.Path))
		require.NoError(t, err)
		var document struct {
			Action generated.Action `json:"action"`
			Ok     *bool            `json:"ok"`
			Error  struct {
				Code generated.ErrorCode `json:"code"`
			} `json:"error"`
			Job struct {
				Status generated.JobStatus `json:"status"`
			} `json:"job"`
		}
		require.NoError(t, json.Unmarshal(raw, &document))
		switch fixture.Schema {
		case "request":
			requestActions[document.Action] = struct{}{}
		case "response":
			if document.Ok != nil && *document.Ok {
				responseActions[document.Action] = struct{}{}
			} else {
				errorCodes[document.Error.Code] = struct{}{}
			}
		case "state":
			stateStatuses[document.Job.Status] = struct{}{}
		default:
			t.Fatalf("unknown fixture schema %q", fixture.Schema)
		}
	}

	actions := []generated.Action{
		generated.ActionRun, generated.ActionWait, generated.ActionStatus, generated.ActionOutput,
		generated.ActionCancel, generated.ActionRemove, generated.ActionList, generated.ActionVersion,
	}
	protocolErrors := []generated.ErrorCode{
		generated.ErrorCodeMalformedJson, generated.ErrorCodeInvalidRequest,
		generated.ErrorCodeIncompatibleVersion, generated.ErrorCodeInvalidRange,
		generated.ErrorCodeInvalidCursor, generated.ErrorCodeJobNotFound, generated.ErrorCodeUnauthorized,
		generated.ErrorCodeActiveJob, generated.ErrorCodeConflict, generated.ErrorCodeCorruptState,
		generated.ErrorCodeRunnerUnavailable, generated.ErrorCodeIoFailure, generated.ErrorCodeInternal,
	}
	statuses := []generated.JobStatus{
		generated.JobStatusRunning, generated.JobStatusSucceeded, generated.JobStatusNonzeroExit,
		generated.JobStatusSignalExit, generated.JobStatusCancelled, generated.JobStatusHardTimeout,
		generated.JobStatusOutputLimit, generated.JobStatusRunnerLost,
	}
	for _, action := range actions {
		_, hasRequest := requestActions[action]
		require.True(t, hasRequest, "missing valid request action %q", action)
		_, hasResponse := responseActions[action]
		require.True(t, hasResponse, "missing valid response action %q", action)
	}
	for _, code := range protocolErrors {
		_, found := errorCodes[code]
		require.True(t, found, "missing valid error code %q", code)
	}
	for _, status := range statuses {
		_, found := stateStatuses[status]
		require.True(t, found, "missing valid state status %q", status)
	}
}

type fixtureCase struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
	Path   string `json:"path"`
	Valid  bool   `json:"valid"`
}

func loadFixtureManifest(t *testing.T, repositoryRoot string) fixtureManifest {
	t.Helper()

	rawManifest, err := os.ReadFile(filepath.Join(repositoryRoot, "fixtures", "v1", "schema", "manifest.json"))
	require.NoError(t, err)

	decoder := json.NewDecoder(bytes.NewReader(rawManifest))
	decoder.DisallowUnknownFields()
	var manifest fixtureManifest
	require.NoError(t, decoder.Decode(&manifest))
	_, err = decoder.Token()
	require.ErrorIs(t, err, io.EOF)
	require.NotEmpty(t, manifest.Cases)

	return manifest
}

func parseFixtureJSON(rawFixture []byte) (any, error) {
	if !utf8.Valid(rawFixture) {
		return nil, errInvalidUTF8
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(rawFixture))
}

package workflow

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	checkoutAction  = "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"
	setupGoAction   = "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e"
	setupNodeAction = "actions/setup-node@820762786026740c76f36085b0efc47a31fe5020"
	setupBunAction  = "oven-sh/setup-bun@0c5077e51419868618aeaa5fe8019c62421857d6"
	uploadAction    = "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a"
	downloadAction  = "actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c"
	sbomAction      = "anchore/sbom-action@e22c389904149dbc22b58101806040fa8d37a610"
	attestAction    = "actions/attest@a1948c3f048ba23858d222213b7c278aabede763"
)

func Test_CIWorkflow_runs_read_only_full_verification_on_pull_requests_and_master(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "ci.yml")

	// When
	triggers := mappingValue(t, workflow, "on")
	verification := job(t, workflow, "verify")

	// Then
	require.Len(t, triggers.Content, 4)
	require.Equal(t, []string{"master"}, sequenceValues(t, mappingValue(t, mappingValue(t, triggers, "pull_request"), "branches")))
	require.Equal(t, []string{"master"}, sequenceValues(t, mappingValue(t, mappingValue(t, triggers, "push"), "branches")))
	_, privilegedTriggerExists := mappingValueIfPresent(triggers, "pull_request_target")
	require.False(t, privilegedTriggerExists)
	permissions := mappingValue(t, workflow, "permissions")
	require.Len(t, permissions.Content, 2)
	require.Equal(t, "read", scalarValue(t, permissions, "contents"))
	_, verificationPermissionsExist := mappingValueIfPresent(verification, "permissions")
	require.False(t, verificationPermissionsExist)
	require.Equal(t, "true", scalarValue(t, mappingValue(t, workflow, "concurrency"), "cancel-in-progress"))
	require.Equal(t, "Verify ${{ matrix.target }}", scalarValue(t, verification, "name"))
	require.Equal(t, "${{ matrix.runner }}", scalarValue(t, verification, "runs-on"))
	require.Equal(t, "90", scalarValue(t, verification, "timeout-minutes"))
	require.Equal(t, "false", scalarValue(t, mappingValue(t, stepByName(t, verification, "Check out source"), "with"), "persist-credentials"))
	require.Equal(t, "1.26.5", scalarValue(t, mappingValue(t, stepByName(t, verification, "Set up Go"), "with"), "go-version"))
	require.Equal(t, "1.3.14", scalarValue(t, mappingValue(t, stepByName(t, verification, "Set up Bun"), "with"), "bun-version"))
	require.Contains(t, scalarValue(t, stepByName(t, verification, "Expose local tools"), "run"), "node_modules/.bin")
	openCodeCheck := scalarValue(t, stepByName(t, verification, "Check OpenCode"), "run")
	require.Equal(t, `test "$(opencode --version)" = "1.18.4"`, openCodeCheck)
	require.NotContains(t, openCodeCheck, "install --global")
	require.Equal(t, `SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)" make verify`, scalarValue(t, stepByName(t, verification, "Run complete verification"), "run"))
}

func Test_ReleaseWorkflow_validates_reviewed_tag_and_verifies_all_native_targets(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")

	// When
	triggers := mappingValue(t, workflow, "on")
	validation := job(t, workflow, "validate")
	verification := job(t, workflow, "verify")

	// Then
	require.Equal(t, []string{"v*"}, sequenceValues(t, mappingValue(t, mappingValue(t, triggers, "push"), "tags")))
	require.Equal(t, "read", scalarValue(t, mappingValue(t, workflow, "permissions"), "contents"))
	require.Equal(t, "read", scalarValue(t, mappingValue(t, validation, "permissions"), "pull-requests"))
	require.Equal(t, "github.event.created == true && github.event.deleted == false", scalarValue(t, validation, "if"))
	require.Equal(t, "false", scalarValue(t, mappingValue(t, stepByName(t, validation, "Check out source"), "with"), "persist-credentials"))
	gate := scalarValue(t, stepByName(t, validation, "Validate release tag"), "run")
	require.Contains(t, gate, "commits/$commit/pulls")
	require.Contains(t, gate, "compare/$commit...master")
	require.Contains(t, gate, "v$version")
	require.Equal(t, []string{"validate"}, scalarOrSequenceValues(t, mappingValue(t, verification, "needs")))
	require.Equal(t, "false", scalarValue(t, mappingValue(t, verification, "strategy"), "fail-fast"))

	include := mappingValue(t, mappingValue(t, mappingValue(t, verification, "strategy"), "matrix"), "include")
	require.Len(t, include.Content, 4)
	runners := make([]string, 0, 4)
	for _, entry := range include.Content {
		runners = append(runners, scalarValue(t, entry, "runner"))
	}
	require.ElementsMatch(t, []string{"ubuntu-24.04", "ubuntu-24.04-arm", "macos-15-intel", "macos-15"}, runners)
	require.Equal(t, "false", scalarValue(t, mappingValue(t, stepByName(t, verification, "Check out source"), "with"), "persist-credentials"))
	require.Equal(t, "1.26.5", scalarValue(t, mappingValue(t, stepByName(t, verification, "Set up Go"), "with"), "go-version"))
	require.Equal(t, "1.3.14", scalarValue(t, mappingValue(t, stepByName(t, verification, "Set up Bun"), "with"), "bun-version"))
	require.Contains(t, scalarValue(t, stepByName(t, verification, "Expose local tools"), "run"), "node_modules/.bin")
	openCodeCheck := scalarValue(t, stepByName(t, verification, "Check OpenCode"), "run")
	require.Equal(t, `test "$(opencode --version)" = "1.18.4"`, openCodeCheck)
	require.NotContains(t, openCodeCheck, "install --global")
	require.Equal(t, "make verify", scalarValue(t, stepByName(t, verification, "Run complete verification"), "run"))
}

func Test_ReleaseWorkflow_publishes_verified_artifact_without_rebuilding(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")
	verification := job(t, workflow, "verify")
	publication := job(t, workflow, "publish")

	// When
	upload := stepByName(t, verification, "Upload verified release bundles")
	download := stepByName(t, publication, "Download verified release bundles")
	publishCommand := scalarValue(t, stepByName(t, publication, "Publish private GitHub release"), "run")

	// Then
	_, conditionalUpload := mappingValueIfPresent(upload, "if")
	require.False(t, conditionalUpload)
	require.Equal(t, "3", scalarValue(t, mappingValue(t, upload, "with"), "retention-days"))
	require.Equal(t, "error", scalarValue(t, mappingValue(t, upload, "with"), "if-no-files-found"))
	require.Contains(t, scalarValue(t, mappingValue(t, upload, "with"), "name"), "matrix.target")
	require.Contains(t, scalarValue(t, mappingValue(t, upload, "with"), "path"), "matrix.target")
	require.Equal(t, downloadAction, scalarValue(t, download, "uses"))
	require.Contains(t, scalarValue(t, mappingValue(t, download, "with"), "pattern"), "release-bundle-")
	require.Equal(t, "true", scalarValue(t, mappingValue(t, download, "with"), "merge-multiple"))
	require.ElementsMatch(t, []string{"validate", "verify"}, scalarOrSequenceValues(t, mappingValue(t, publication, "needs")))
	require.Equal(t, "write", scalarValue(t, mappingValue(t, publication, "permissions"), "contents"))
	require.Contains(t, publishCommand, "sha256sum agent-managed-bash-*.tar.gz > SHA256SUMS")
	require.Contains(t, publishCommand, "sha256sum -c SHA256SUMS")
	require.Contains(t, publishCommand, "git/ref/tags/$RELEASE_TAG")
	require.Contains(t, publishCommand, `test "$tag_sha" = "$VERIFIED_COMMIT"`)
	require.Contains(t, publishCommand, "gh release create")
	require.Contains(t, publishCommand, "--verify-tag")
	for _, step := range mappingValue(t, publication, "steps").Content {
		if run, ok := mappingValueIfPresent(step, "run"); ok {
			require.NotContains(t, run.Value, "make release-package")
			require.NotContains(t, run.Value, "make verify")
		}
	}
}

func Test_Workflows_pin_remote_actions_to_approved_commits(t *testing.T) {
	// Given
	ciWorkflow := loadWorkflow(t, "ci.yml")
	releaseWorkflow := loadWorkflow(t, "release.yml")

	// When
	err := validateApprovedRemoteActions(ciWorkflow, releaseWorkflow)

	// Then
	require.NoError(t, err)
}

func Test_Makefile_includes_workflow_contract_in_complete_verification(t *testing.T) {
	// Given
	makefile, err := os.ReadFile("../../Makefile")
	require.NoError(t, err)

	// When
	contents := string(makefile)

	// Then
	require.Contains(t, contents, "workflow-test:")
	require.Contains(t, contents, "go tool actionlint")
	require.Contains(t, contents, "$(MAKE) --no-print-directory workflow-test")
}

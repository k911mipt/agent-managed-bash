package workflow

import (
	"os"
	"regexp"
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
	require.Equal(t, "go.mod", scalarValue(t, mappingValue(t, stepByName(t, verification, "Set up Go"), "with"), "go-version-file"))
	require.Equal(t, ".bun-version", scalarValue(t, mappingValue(t, stepByName(t, verification, "Set up Bun"), "with"), "bun-version-file"))
	installTmux := stepByName(t, verification, "Install tmux on macOS")
	require.Equal(t, "runner.os == 'macOS'", scalarValue(t, installTmux, "if"))
	require.Equal(t, "brew install tmux", scalarValue(t, installTmux, "run"))
	require.Contains(t, scalarValue(t, stepByName(t, verification, "Expose local tools"), "run"), "node_modules/.bin")
	openCodeCheck := scalarValue(t, stepByName(t, verification, "Check OpenCode"), "run")
	require.Contains(t, openCodeCheck, `.devDependencies["opencode-ai"]`)
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
	producer := job(t, workflow, "produce")

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
	require.Equal(t, []string{"validate", "fresh-guard"}, scalarOrSequenceValues(t, mappingValue(t, verification, "needs")))
	require.Equal(t, "false", scalarValue(t, mappingValue(t, verification, "strategy"), "fail-fast"))

	include := mappingValue(t, mappingValue(t, mappingValue(t, verification, "strategy"), "matrix"), "include")
	require.Len(t, include.Content, 4)
	runners := make([]string, 0, 4)
	for _, entry := range include.Content {
		runners = append(runners, scalarValue(t, entry, "runner"))
	}
	require.ElementsMatch(t, []string{"ubuntu-24.04", "ubuntu-24.04-arm", "macos-15-intel", "macos-15"}, runners)
	require.Equal(t, "false", scalarValue(t, mappingValue(t, stepByName(t, verification, "Check out source"), "with"), "persist-credentials"))
	require.Equal(t, "go.mod", scalarValue(t, mappingValue(t, stepByName(t, verification, "Set up Go"), "with"), "go-version-file"))
	require.Equal(t, ".bun-version", scalarValue(t, mappingValue(t, stepByName(t, verification, "Set up Bun"), "with"), "bun-version-file"))
	installTmux := stepByName(t, verification, "Install tmux on macOS")
	require.Equal(t, "runner.os == 'macOS'", scalarValue(t, installTmux, "if"))
	require.Equal(t, "brew install tmux", scalarValue(t, installTmux, "run"))
	require.Contains(t, scalarValue(t, stepByName(t, verification, "Expose local tools"), "run"), "node_modules/.bin")
	openCodeCheck := scalarValue(t, stepByName(t, verification, "Check OpenCode"), "run")
	require.Contains(t, openCodeCheck, `.devDependencies["opencode-ai"]`)
	require.NotContains(t, openCodeCheck, "install --global")
	require.Equal(t, "make verify", scalarValue(t, stepByName(t, verification, "Run complete verification"), "run"))
	for _, step := range mappingValue(t, verification, "steps").Content {
		if uses, ok := mappingValueIfPresent(step, "uses"); ok {
			require.NotEqual(t, uploadAction, uses.Value)
		}
	}
	require.Equal(t, []string{"validate", "verify"}, scalarOrSequenceValues(t, mappingValue(t, producer, "needs")))
	producerInclude := mappingValue(t, mappingValue(t, mappingValue(t, producer, "strategy"), "matrix"), "include")
	require.Len(t, producerInclude.Content, 4)
	producerTargets := make([]string, 0, 4)
	producerRunners := make([]string, 0, 4)
	for _, entry := range producerInclude.Content {
		producerTargets = append(producerTargets, scalarValue(t, entry, "target"))
		producerRunners = append(producerRunners, scalarValue(t, entry, "runner"))
	}
	require.ElementsMatch(t, []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64"}, producerTargets)
	require.ElementsMatch(t, []string{"ubuntu-24.04", "ubuntu-24.04-arm", "macos-15-intel", "macos-15"}, producerRunners)
	producerReceipt := scalarValue(t, stepByName(t, producer, "Create release producer receipts"), "run")
	require.Contains(t, producerReceipt, "make release-package")
	require.NotContains(t, producerReceipt, "make verify")
	require.Contains(t, producerReceipt, `cp "dist/npm/$npm_asset" "dist/$npm_asset"`)
	for _, step := range mappingValue(t, producer, "steps").Content {
		if name, ok := mappingValueIfPresent(step, "name"); ok {
			require.NotEqual(t, "Install tmux on macOS", name.Value)
		}
	}
	scanner := job(t, workflow, "sbom")
	prepare := job(t, workflow, "prepare")
	require.Equal(t, []string{"validate", "produce"}, scalarOrSequenceValues(t, mappingValue(t, scanner, "needs")))
	require.Equal(t, []string{"validate", "produce", "sbom"}, scalarOrSequenceValues(t, mappingValue(t, prepare, "needs")))
}

func Test_ReleaseWorkflow_creates_candidate_artifacts_without_external_publication(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")
	verification := job(t, workflow, "verify")
	producer := job(t, workflow, "produce")
	prepare := job(t, workflow, "prepare")
	control := job(t, workflow, "control")

	// When
	upload := stepByName(t, prepare, "Upload release candidate")

	// Then
	require.Equal(t, "90", scalarValue(t, mappingValue(t, upload, "with"), "retention-days"))
	require.Equal(t, "read", scalarValue(t, mappingValue(t, prepare, "permissions"), "contents"))
	require.Equal(t, "read", scalarValue(t, mappingValue(t, control, "permissions"), "contents"))
	require.Contains(t, scalarValue(t, mappingValue(t, stepByName(t, control, "Upload candidate control receipt"), "with"), "path"), "CANDIDATE-RECEIPT.json")
	for _, jobNode := range []*yaml.Node{verification, prepare, control} {
		for _, step := range mappingValue(t, jobNode, "steps").Content {
			if uses, ok := mappingValueIfPresent(step, "uses"); ok {
				require.NotContains(t, uses.Value, "actions/attest")
			}
			if run, ok := mappingValueIfPresent(step, "run"); ok {
				require.NotContains(t, run.Value, "make release-package")
				require.NotContains(t, run.Value, "gh release")
				require.NotContains(t, run.Value, "npm publish")
			}
		}
	}
	for _, step := range mappingValue(t, producer, "steps").Content {
		if uses, ok := mappingValueIfPresent(step, "uses"); ok {
			require.NotContains(t, uses.Value, "actions/attest")
		}
		if run, ok := mappingValueIfPresent(step, "run"); ok {
			require.NotContains(t, run.Value, "gh release")
			require.NotContains(t, run.Value, "npm publish")
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

func Test_Makefile_complete_verification_runs_required_release_contracts(t *testing.T) {
	// Given
	makefile, err := os.ReadFile("../../Makefile")
	require.NoError(t, err)
	recipe := regexp.MustCompile(`(?m)^verify:\n(?:\t.*\n)+`).FindString(string(makefile))

	// When
	requiredTargets := []string{
		"workflow-test",
		"npm-package-test",
		"public-install-test",
		"release-candidate-test",
		"release-publish-test",
	}

	// Then
	require.NotEmpty(t, recipe)
	for _, target := range requiredTargets {
		require.Contains(t, string(makefile), target+":")
		require.Contains(t, recipe, "$(MAKE) --no-print-directory "+target)
	}
	require.Contains(t, string(makefile), "go tool actionlint")
}

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_ReleaseWorkflow_stages_and_finalizes_through_separate_release_approvals(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")

	// When
	stage := job(t, workflow, "stage-release")
	finalize := job(t, workflow, "finalize-release")

	// Then
	require.Equal(t, "release", scalarValue(t, stage, "environment"))
	require.Equal(t, "release", scalarValue(t, finalize, "environment"))
	require.Equal(t, "write", scalarValue(t, mappingValue(t, stage, "permissions"), "contents"))
	require.Equal(t, "write", scalarValue(t, mappingValue(t, stage, "permissions"), "attestations"))
	require.Equal(t, "write", scalarValue(t, mappingValue(t, stage, "permissions"), "id-token"))
	require.Equal(t, "write", scalarValue(t, mappingValue(t, finalize, "permissions"), "contents"))
	require.Len(t, mappingValue(t, finalize, "permissions").Content, 2)
	require.Equal(t, attestAction, scalarValue(t, stepByName(t, stage, "Attest all release subjects"), "uses"))
	provenancePaths := scalarValue(t, mappingValue(t, stepByName(t, stage, "Attest all release subjects"), "with"), "subject-path")
	require.Equal(t, "${{ steps.publication.outputs.provenance }}", provenancePaths)
	for _, name := range []string{"Attest Linux amd64 SBOM", "Attest Linux arm64 SBOM", "Attest Darwin amd64 SBOM", "Attest Darwin arm64 SBOM", "Attest npm SBOM"} {
		require.Equal(t, attestAction, scalarValue(t, stepByName(t, stage, name), "uses"))
	}
}

func Test_ReleaseWorkflow_recovery_requires_original_artifacts_without_producers(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")

	// When
	triggers := mappingValue(t, workflow, "on")
	recovery := job(t, workflow, "recovery")

	// Then
	inputs := mappingValue(t, mappingValue(t, triggers, "workflow_dispatch"), "inputs")
	require.Equal(t, "true", scalarValue(t, mappingValue(t, inputs, "recovery_run_id"), "required"))
	require.Equal(t, "true", scalarValue(t, mappingValue(t, inputs, "tag"), "required"))
	require.Contains(t, scalarValue(t, stepByName(t, recovery, "Validate original candidate and receipt"), "run"), "release-publish.ts recovery")
	for _, step := range mappingValue(t, recovery, "steps").Content {
		if run, exists := mappingValueIfPresent(step, "run"); exists {
			require.NotContains(t, run.Value, "make verify")
			require.NotContains(t, run.Value, "release-candidate.ts assemble")
			require.NotContains(t, run.Value, "sbom-action")
		}
	}
}

func Test_ReleaseWorkflow_recovery_uses_the_original_tagged_run_commit(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")
	recovery := job(t, workflow, "recovery")
	stage := job(t, workflow, "stage-release")
	finalize := job(t, workflow, "finalize-release")

	// When
	recoveryCommand := scalarValue(t, stepByName(t, recovery, "Validate original workflow run"), "run")

	// Then
	recoveryEnvironment := mappingValue(t, stepByName(t, recovery, "Validate original candidate and receipt"), "env")
	require.Contains(t, recoveryCommand, "refs/tags/$RELEASE_TAG")
	require.NotContains(t, recoveryCommand, "github.sha")
	require.Contains(t, scalarValue(t, mappingValue(t, stage, "env"), "RELEASE_COMMIT"), "needs.recovery.outputs.commit")
	require.Contains(t, scalarValue(t, mappingValue(t, finalize, "env"), "RELEASE_COMMIT"), "needs.recovery.outputs.commit")
	require.Equal(t, "${{ steps.run.outputs.workflow-blob }}", scalarValue(t, recoveryEnvironment, "RELEASE_WORKFLOW_BLOB"))
}

func Test_ReleaseWorkflow_fresh_guard_fails_closed_on_non_absence(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")
	guard := job(t, workflow, "fresh-guard")

	// When
	command := scalarValue(t, stepByName(t, guard, "Read only npm and release guard"), "run")

	// Then
	require.Equal(t, "set -eu\nbun scripts/release-publish.ts guard\n", command)
}

func Test_ReleaseWorkflow_pins_npm_for_all_publication_reads(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")

	// When
	jobs := []string{"fresh-guard", "stage-release", "finalize-release"}

	// Then
	for _, name := range jobs {
		jobNode := job(t, workflow, name)
		setup := stepByName(t, jobNode, "Set up Node")
		require.Equal(t, setupNodeAction, scalarValue(t, setup, "uses"))
		require.Equal(t, "24.18.0", scalarValue(t, mappingValue(t, setup, "with"), "node-version"))
		require.Equal(t, `test "$(npm --version)" = "11.5.1"`, scalarValue(t, stepByName(t, jobNode, "Check pinned npm"), "run"))
	}
}

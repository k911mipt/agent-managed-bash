package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func Test_ReleaseWorkflow_creates_a_receipted_scanner_isolated_candidate(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")

	// When
	verification := job(t, workflow, "verify")
	scanner := job(t, workflow, "sbom")
	prepare := job(t, workflow, "prepare")
	control := job(t, workflow, "control")

	// Then
	producerCommand := scalarValue(t, stepByName(t, verification, "Create release producer receipts"), "run")
	require.Contains(t, producerCommand, "make verify")
	require.Contains(t, producerCommand, "release-candidate.ts producer")
	require.Equal(t, "false", scalarValue(t, mappingValue(t, stepByName(t, scanner, "Check out source"), "with"), "persist-credentials"))
	require.Equal(t, "anchore/sbom-action@e22c389904149dbc22b58101806040fa8d37a610", scalarValue(t, stepByName(t, scanner, "Generate SPDX SBOM"), "uses"))
	require.Contains(t, scalarValue(t, stepByName(t, scanner, "Create SBOM relation receipt"), "run"), "release-candidate.ts relation")
	require.Contains(t, scalarValue(t, stepByName(t, prepare, "Assemble release candidate"), "run"), "release-candidate.ts assemble")
	require.Contains(t, scalarValue(t, stepByName(t, control, "Create candidate control receipt"), "run"), "release-candidate.ts control")

	for _, jobNode := range []*yaml.Node{prepare, control} {
		for _, step := range mappingValue(t, jobNode, "steps").Content {
			if run, exists := mappingValueIfPresent(step, "run"); exists {
				require.NotContains(t, run.Value, "make verify")
				require.NotContains(t, run.Value, "make release-package")
				require.NotContains(t, run.Value, "bun pm pack")
				require.NotContains(t, run.Value, "syft")
			}
		}
	}
}

func Test_ReleaseWorkflow_binds_control_through_trusted_environment(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")
	prepare := job(t, workflow, "prepare")
	control := job(t, workflow, "control")

	// When
	prepareCommand := scalarValue(t, stepByName(t, prepare, "Assemble release candidate"), "run")
	metadataCommand := scalarValue(t, stepByName(t, prepare, "Create private candidate metadata"), "run")
	controlCommand := scalarValue(t, stepByName(t, control, "Create candidate control receipt"), "run")

	// Then
	for _, step := range []*yaml.Node{
		stepByName(t, prepare, "Assemble release candidate"),
		stepByName(t, prepare, "Create private candidate metadata"),
		stepByName(t, control, "Create candidate control receipt"),
	} {
		environment := mappingValue(t, step, "env")
		require.Equal(t, "${{ github.repository }}", scalarValue(t, environment, "RELEASE_REPOSITORY"))
		require.Equal(t, "release.yml", scalarValue(t, environment, "RELEASE_WORKFLOW"))
		require.Equal(t, "${{ github.run_id }}", scalarValue(t, environment, "RELEASE_RUN_ID"))
		require.Equal(t, "${{ github.run_attempt }}", scalarValue(t, environment, "RELEASE_RUN_ATTEMPT"))
		require.Equal(t, "${{ github.ref_name }}", scalarValue(t, environment, "RELEASE_TAG"))
		require.Equal(t, "${{ github.sha }}", scalarValue(t, environment, "RELEASE_COMMIT"))
		require.Equal(t, "${{ needs.validate.outputs.version }}", scalarValue(t, environment, "RELEASE_VERSION"))
	}
	controlEnvironment := mappingValue(t, stepByName(t, control, "Create candidate control receipt"), "env")
	require.Equal(t, "${{ needs.prepare.outputs.candidate-id }}", scalarValue(t, controlEnvironment, "RELEASE_CANDIDATE_ARTIFACT_ID"))
	require.Equal(t, "${{ needs.prepare.outputs.candidate-digest }}", scalarValue(t, controlEnvironment, "RELEASE_CANDIDATE_ARTIFACT_DIGEST"))
	require.NotContains(t, prepareCommand, "${{ github.")
	require.NotContains(t, metadataCommand, "${{ github.")
	require.NotContains(t, controlCommand, "${{ github.")
	require.Contains(t, metadataCommand, "$RELEASE_CANDIDATE_ARTIFACT_ID")
	require.Contains(t, metadataCommand, "$RELEASE_CANDIDATE_ARTIFACT_DIGEST")
	require.Contains(t, controlCommand, "$RELEASE_CANDIDATE_ARTIFACT_ID")
	require.Contains(t, controlCommand, "$RELEASE_CANDIDATE_ARTIFACT_DIGEST")
}

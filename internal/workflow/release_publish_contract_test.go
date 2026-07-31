package workflow

import (
	"strings"
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

func Test_ReleaseWorkflow_scopes_oidc_and_npm_publication_to_the_release_environment(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")
	jobs := mappingValue(t, workflow, "jobs")
	_, workflowOIDC := mappingValueIfPresent(mappingValue(t, workflow, "permissions"), "id-token")
	_, workflowCredential := mappingValueIfPresent(mappingValue(t, workflow, "env"), "NPM_TOKEN")

	// When
	oidcJobs := make([]string, 0, 1)
	publicationJobs := make([]string, 0, 1)
	credentialJobs := make([]string, 0, 1)
	for index := 0; index < len(jobs.Content); index += 2 {
		name := jobs.Content[index].Value
		workflowJob := jobs.Content[index+1]
		if environment, exists := mappingValueIfPresent(workflowJob, "env"); exists {
			_, jobCredential := mappingValueIfPresent(environment, "NPM_TOKEN")
			require.Falsef(t, jobCredential, "job %s must not inherit npm credentials", name)
		}
		if permissions, exists := mappingValueIfPresent(workflowJob, "permissions"); exists {
			if idToken, exists := mappingValueIfPresent(permissions, "id-token"); exists && idToken.Value == "write" {
				oidcJobs = append(oidcJobs, name)
				require.Equal(t, "release", scalarValue(t, workflowJob, "environment"))
			}
		}
		steps := mappingValue(t, workflowJob, "steps")
		for _, step := range steps.Content {
			if run, exists := mappingValueIfPresent(step, "run"); exists && strings.Contains(run.Value, "release-publish.ts stage") {
				publicationJobs = append(publicationJobs, name)
			}
			if environment, exists := mappingValueIfPresent(step, "env"); exists {
				if _, exists := mappingValueIfPresent(environment, "NPM_TOKEN"); exists {
					credentialJobs = append(credentialJobs, name)
				}
			}
		}
	}

	// Then
	require.False(t, workflowOIDC)
	require.False(t, workflowCredential)
	require.Equal(t, []string{"stage-release"}, oidcJobs)
	require.Equal(t, []string{"stage-release"}, publicationJobs)
	require.Equal(t, []string{"stage-release"}, credentialJobs)
}

func Test_ReleaseWorkflow_scopes_read_delay_to_publication_jobs(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "release.yml")

	// When
	globalEnvironment := mappingValue(t, workflow, "env")
	_, globallyConfigured := mappingValueIfPresent(globalEnvironment, "RELEASE_READ_DELAY_MS")

	// Then
	require.False(t, globallyConfigured)
	for _, name := range []string{"stage-release", "finalize-release"} {
		require.Equal(t, "20000", scalarValue(t, mappingValue(t, job(t, workflow, name), "env"), "RELEASE_READ_DELAY_MS"))
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
	toolchain := mappingValue(t, workflow, "env")

	// When
	jobs := []string{"fresh-guard", "stage-release", "finalize-release"}

	// Then
	require.Equal(t, "24.18.0", scalarValue(t, toolchain, "NODE_VERSION"))
	require.Equal(t, "11.5.1", scalarValue(t, toolchain, "NPM_VERSION"))
	for _, name := range jobs {
		jobNode := job(t, workflow, name)
		setup := stepByName(t, jobNode, "Set up Node")
		require.Equal(t, setupNodeAction, scalarValue(t, setup, "uses"))
		require.Equal(t, "${{ env.NODE_VERSION }}", scalarValue(t, mappingValue(t, setup, "with"), "node-version"))
		require.Equal(t, `npm install --global "npm@$NPM_VERSION"`, scalarValue(t, stepByName(t, jobNode, "Install pinned npm"), "run"))
		require.Equal(t, `test "$(npm --version)" = "$NPM_VERSION"`, scalarValue(t, stepByName(t, jobNode, "Check pinned npm"), "run"))
	}
}

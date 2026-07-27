package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_CIWorkflow_has_exact_supported_verification_matrix(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "ci.yml")

	// When
	err := validateCIVerificationMatrix(workflow)

	// Then
	require.NoError(t, err)
}

func Test_CIWorkflow_verification_gate_accepts_success_and_rejects_other_results(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "ci.yml")
	tests := []struct {
		name     string
		result   string
		exitCode int
	}{
		{name: "success", result: "success", exitCode: 0},
		{name: "failure", result: "failure", exitCode: 1},
		{name: "skipped", result: "skipped", exitCode: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			exitCode, err := verificationGateExitCode(workflow, test.result)

			// Then
			require.NoError(t, err)
			require.Equal(t, test.exitCode, exitCode)
		})
	}
}

func Test_CIWorkflowParser_rejects_malformed_yaml(t *testing.T) {
	// Given
	fixture := []byte("jobs: [")

	// When
	_, err := parseWorkflow(fixture)

	// Then
	require.Error(t, err)
}

func Test_CIWorkflowMatrix_rejects_incomplete_fixture(t *testing.T) {
	// Given
	workflow, err := parseWorkflow([]byte(`
jobs:
  verify:
    strategy:
      matrix:
        include:
          - target: linux-amd64
            runner: ubuntu-24.04
`))
	require.NoError(t, err)

	// When
	err = validateCIVerificationMatrix(workflow)

	// Then
	require.Error(t, err)
}

func Test_CIWorkflowMatrix_rejects_malformed_fixture(t *testing.T) {
	// Given
	workflow, err := parseWorkflow([]byte(`
jobs:
  verify:
    strategy:
      matrix:
        include: linux-amd64
`))
	require.NoError(t, err)

	// When
	err = validateCIVerificationMatrix(workflow)

	// Then
	require.Error(t, err)
}

func Test_CIWorkflowVerificationGate_rejects_duplicate_gate_fixture(t *testing.T) {
	// Given
	workflow, err := parseWorkflow([]byte(`
jobs:
  first:
    name: Verification gate
  second:
    name: Verification gate
`))
	require.NoError(t, err)

	// When
	_, err = verificationGateCommand(workflow)

	// Then
	require.Error(t, err)
}

func Test_CIWorkflowVerificationGate_rejects_unknown_result_fixture(t *testing.T) {
	// Given
	workflow := loadWorkflow(t, "ci.yml")

	// When
	_, err := verificationGateExitCode(workflow, "unexpected")

	// Then
	require.Error(t, err)
}

func Test_WorkflowActionAllowlist_rejects_unapproved_reference(t *testing.T) {
	// Given
	workflow, err := parseWorkflow([]byte(`
jobs:
  verify:
    steps:
      - uses: actions/checkout@v4
`))
	require.NoError(t, err)

	// When
	err = validateApprovedRemoteActions(workflow)

	// Then
	require.Error(t, err)
}

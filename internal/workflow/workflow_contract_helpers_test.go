package workflow

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

var expectedCIVerificationRunners = map[string]string{
	"linux-amd64":  "ubuntu-24.04",
	"linux-arm64":  "ubuntu-24.04-arm",
	"darwin-amd64": "macos-15-intel",
	"darwin-arm64": "macos-15",
}

var approvedRemoteActions = map[string]struct{}{
	checkoutAction:  {},
	setupGoAction:   {},
	setupNodeAction: {},
	setupBunAction:  {},
	uploadAction:    {},
	downloadAction:  {},
	sbomAction:      {},
	attestAction:    {},
}

func validateCIVerificationMatrix(workflow *yaml.Node) error {
	verification, err := workflowJob(workflow, "verify")
	if err != nil {
		return err
	}
	strategy, err := workflowMappingValue(verification, "strategy")
	if err != nil {
		return err
	}
	matrix, err := workflowMappingValue(strategy, "matrix")
	if err != nil {
		return err
	}
	include, err := workflowMappingValue(matrix, "include")
	if err != nil {
		return err
	}
	if include.Kind != yaml.SequenceNode {
		return fmt.Errorf("matrix include is not a sequence")
	}
	if len(include.Content) != len(expectedCIVerificationRunners) {
		return fmt.Errorf("matrix has %d entries", len(include.Content))
	}

	actual := make(map[string]string, len(include.Content))
	for _, entry := range include.Content {
		target, err := workflowScalarValue(entry, "target")
		if err != nil {
			return err
		}
		runner, err := workflowScalarValue(entry, "runner")
		if err != nil {
			return err
		}
		if _, exists := actual[target]; exists {
			return fmt.Errorf("matrix repeats target %q", target)
		}
		actual[target] = runner
	}

	for target, runner := range expectedCIVerificationRunners {
		if actual[target] != runner {
			return fmt.Errorf("matrix runner for %q is %q", target, actual[target])
		}
	}
	return nil
}

func verificationGateExitCode(workflow *yaml.Node, result string) (int, error) {
	command, err := verificationGateCommand(workflow)
	if err != nil {
		return -1, err
	}
	if result != "success" && result != "failure" && result != "skipped" {
		return -1, fmt.Errorf("unknown verification result %q", result)
	}

	command = strings.ReplaceAll(command, "${{ needs.verify.result }}", result)
	err = exec.Command("sh", "-c", command).Run()
	if err == nil {
		return 0, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), nil
	}
	return -1, fmt.Errorf("run verification gate: %w", err)
}

func verificationGateCommand(workflow *yaml.Node) (string, error) {
	jobs, err := workflowMappingValue(workflow, "jobs")
	if err != nil {
		return "", err
	}
	var gates []*yaml.Node
	for index := 1; index < len(jobs.Content); index += 2 {
		name, err := workflowScalarValue(jobs.Content[index], "name")
		if err == nil && name == "Verification gate" {
			gates = append(gates, jobs.Content[index])
		}
	}
	if len(gates) != 1 {
		return "", fmt.Errorf("found %d verification gates", len(gates))
	}
	gate := gates[0]
	if needs, err := workflowScalarValue(gate, "needs"); err != nil || needs != "verify" {
		return "", fmt.Errorf("verification gate must need verify")
	}
	if condition, err := workflowScalarValue(gate, "if"); err != nil || condition != "always()" {
		return "", fmt.Errorf("verification gate must always run")
	}
	permissions, err := workflowMappingValue(gate, "permissions")
	if err != nil {
		return "", err
	}
	if len(permissions.Content) != 2 {
		return "", fmt.Errorf("verification gate has extra permissions")
	}
	if contents, err := workflowScalarValue(permissions, "contents"); err != nil || contents != "read" {
		return "", fmt.Errorf("verification gate permissions are not read-only")
	}
	steps, err := workflowMappingValue(gate, "steps")
	if err != nil || steps.Kind != yaml.SequenceNode || len(steps.Content) != 1 {
		return "", fmt.Errorf("verification gate must have one step")
	}
	if _, usesCheckout := mappingValueIfPresent(steps.Content[0], "uses"); usesCheckout {
		return "", fmt.Errorf("verification gate must not check out source")
	}
	command, err := workflowScalarValue(steps.Content[0], "run")
	if err != nil || command != `test "${{ needs.verify.result }}" = "success"` {
		return "", fmt.Errorf("verification gate has an invalid command")
	}
	return command, nil
}

func validateApprovedRemoteActions(workflows ...*yaml.Node) error {
	var uses []string
	for _, workflow := range workflows {
		uses = append(uses, remoteActionUses(workflow)...)
	}
	if len(uses) == 0 {
		return fmt.Errorf("workflow has no remote action references")
	}
	for _, action := range uses {
		if _, approved := approvedRemoteActions[action]; !approved {
			return fmt.Errorf("unapproved action reference: %s", action)
		}
		parts := strings.Split(action, "@")
		if len(parts) != 2 || len(parts[1]) != 40 {
			return fmt.Errorf("action is not pinned to a full SHA: %s", action)
		}
	}
	return nil
}

func parseWorkflow(data []byte) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("unmarshal workflow: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("workflow document is not a mapping")
	}
	return document.Content[0], nil
}

func workflowJob(workflow *yaml.Node, name string) (*yaml.Node, error) {
	jobs, err := workflowMappingValue(workflow, "jobs")
	if err != nil {
		return nil, err
	}
	return workflowMappingValue(jobs, name)
}

func workflowScalarValue(mapping *yaml.Node, key string) (string, error) {
	value, err := workflowMappingValue(mapping, key)
	if err != nil {
		return "", err
	}
	if value.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("mapping key %q is not scalar", key)
	}
	return value.Value, nil
}

func workflowMappingValue(mapping *yaml.Node, key string) (*yaml.Node, error) {
	if mapping.Kind != yaml.MappingNode || len(mapping.Content)%2 != 0 {
		return nil, fmt.Errorf("node is not a valid mapping")
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1], nil
		}
	}
	return nil, fmt.Errorf("mapping key %q is missing", key)
}

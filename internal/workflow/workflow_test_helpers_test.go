package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func loadWorkflow(t *testing.T, filename string) *yaml.Node {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", filename))
	require.NoError(t, err)

	var document yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &document))
	require.Len(t, document.Content, 1)
	require.Equal(t, yaml.MappingNode, document.Content[0].Kind)

	return document.Content[0]
}

func mappingValue(t *testing.T, mapping *yaml.Node, key string) *yaml.Node {
	t.Helper()
	require.Equal(t, yaml.MappingNode, mapping.Kind)

	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}

	require.FailNow(t, "mapping key is missing", "key=%s", key)
	return nil
}

func mappingValueIfPresent(mapping *yaml.Node, key string) (*yaml.Node, bool) {
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1], true
		}
	}

	return nil, false
}

func scalarValue(t *testing.T, mapping *yaml.Node, key string) string {
	t.Helper()

	value := mappingValue(t, mapping, key)
	require.Equal(t, yaml.ScalarNode, value.Kind)
	return value.Value
}

func sequenceValues(t *testing.T, sequence *yaml.Node) []string {
	t.Helper()
	require.Equal(t, yaml.SequenceNode, sequence.Kind)

	values := make([]string, 0, len(sequence.Content))
	for _, item := range sequence.Content {
		require.Equal(t, yaml.ScalarNode, item.Kind)
		values = append(values, item.Value)
	}
	return values
}

func scalarOrSequenceValues(t *testing.T, node *yaml.Node) []string {
	t.Helper()

	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}
	return sequenceValues(t, node)
}

func job(t *testing.T, workflow *yaml.Node, name string) *yaml.Node {
	t.Helper()
	return mappingValue(t, mappingValue(t, workflow, "jobs"), name)
}

func stepByName(t *testing.T, workflowJob *yaml.Node, name string) *yaml.Node {
	t.Helper()

	steps := mappingValue(t, workflowJob, "steps")
	require.Equal(t, yaml.SequenceNode, steps.Kind)
	for _, step := range steps.Content {
		if stepName, ok := mappingValueIfPresent(step, "name"); ok && stepName.Value == name {
			return step
		}
	}

	require.FailNow(t, "workflow step is missing", "name=%s", name)
	return nil
}

func remoteActionUses(workflow *yaml.Node) []string {
	jobs, ok := mappingValueIfPresent(workflow, "jobs")
	if !ok {
		return nil
	}

	var uses []string
	for index := 1; index < len(jobs.Content); index += 2 {
		steps, ok := mappingValueIfPresent(jobs.Content[index], "steps")
		if !ok || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			value, ok := mappingValueIfPresent(step, "uses")
			if ok {
				uses = append(uses, value.Value)
			}
		}
	}
	return uses
}

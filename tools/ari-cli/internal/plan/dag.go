package plan

import (
	"fmt"
	"sort"
	"strings"
)

// TopologicalSort sorts steps by dependencies using Kahn's algorithm.
// Returns ordered slice where all dependencies come before dependents.
// Returns error if dependency references are invalid or cycles are detected.
func TopologicalSort(steps []Step) ([]Step, error) {
	stepByID := make(map[string]Step, len(steps))
	inputIndex := make(map[string]int, len(steps))
	for i, step := range steps {
		if step.StepID == "" {
			return nil, fmt.Errorf("step at index %d has empty step_id", i)
		}
		if first, exists := inputIndex[step.StepID]; exists {
			return nil, fmt.Errorf("duplicate step_id %q at index %d (first seen at index %d)", step.StepID, i, first)
		}
		stepByID[step.StepID] = step
		inputIndex[step.StepID] = i
	}

	indegree := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps))
	for _, step := range steps {
		indegree[step.StepID] = 0
	}

	for _, step := range steps {
		for _, depID := range step.DependsOn {
			if _, exists := stepByID[depID]; !exists {
				return nil, fmt.Errorf("step %q depends on unknown step_id %q", step.StepID, depID)
			}
			indegree[step.StepID]++
			dependents[depID] = append(dependents[depID], step.StepID)
		}
	}

	queue := make([]string, 0, len(steps))
	for _, step := range steps {
		if indegree[step.StepID] == 0 {
			queue = append(queue, step.StepID)
		}
	}

	ordered := make([]Step, 0, len(steps))
	for len(queue) > 0 {
		stepID := queue[0]
		queue = queue[1:]
		ordered = append(ordered, stepByID[stepID])

		for _, dependentID := range dependents[stepID] {
			indegree[dependentID]--
			if indegree[dependentID] == 0 {
				queue = append(queue, dependentID)
			}
		}
	}

	if len(ordered) == len(steps) {
		return ordered, nil
	}

	remaining := make([]string, 0, len(steps)-len(ordered))
	for stepID, degree := range indegree {
		if degree > 0 {
			remaining = append(remaining, stepID)
		}
	}
	sort.Slice(remaining, func(i, j int) bool {
		return inputIndex[remaining[i]] < inputIndex[remaining[j]]
	})

	return nil, fmt.Errorf("cycle detected among steps: %s", strings.Join(remaining, " -> "))
}

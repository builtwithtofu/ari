package plan

import (
	"strings"
	"testing"
)

func TestTopologicalSort_LinearChain(t *testing.T) {
	steps := []Step{
		{StepID: "C", DependsOn: []string{"B"}},
		{StepID: "B", DependsOn: []string{"A"}},
		{StepID: "A"},
	}

	ordered, err := TopologicalSort(steps)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertStepOrder(t, ordered, []string{"A", "B", "C"})
}

func TestTopologicalSort_DiamondPattern(t *testing.T) {
	steps := []Step{
		{StepID: "D", DependsOn: []string{"B", "C"}},
		{StepID: "B", DependsOn: []string{"A"}},
		{StepID: "C", DependsOn: []string{"A"}},
		{StepID: "A"},
	}

	ordered, err := TopologicalSort(steps)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertTopologicalConstraints(t, ordered, map[string][]string{
		"B": []string{"A"},
		"C": []string{"A"},
		"D": []string{"B", "C"},
	})
}

func TestTopologicalSort_ComplexDAG(t *testing.T) {
	steps := []Step{
		{StepID: "F", DependsOn: []string{"D", "E"}},
		{StepID: "E", DependsOn: []string{"C"}},
		{StepID: "D", DependsOn: []string{"B", "C"}},
		{StepID: "C", DependsOn: []string{"A"}},
		{StepID: "B", DependsOn: []string{"A"}},
		{StepID: "A"},
	}

	ordered, err := TopologicalSort(steps)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertTopologicalConstraints(t, ordered, map[string][]string{
		"B": []string{"A"},
		"C": []string{"A"},
		"D": []string{"B", "C"},
		"E": []string{"C"},
		"F": []string{"D", "E"},
	})
}

func TestTopologicalSort_CircularDependency(t *testing.T) {
	steps := []Step{
		{StepID: "A", DependsOn: []string{"C"}},
		{StepID: "B", DependsOn: []string{"A"}},
		{StepID: "C", DependsOn: []string{"B"}},
	}

	ordered, err := TopologicalSort(steps)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if ordered != nil {
		t.Fatalf("expected nil ordered result, got %v", ordered)
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("expected cycle error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "A") || !strings.Contains(err.Error(), "B") || !strings.Contains(err.Error(), "C") {
		t.Fatalf("expected cycle error to include cycle steps, got %q", err.Error())
	}
}

func assertStepOrder(t *testing.T, steps []Step, want []string) {
	t.Helper()
	if len(steps) != len(want) {
		t.Fatalf("expected %d steps, got %d", len(want), len(steps))
	}
	for i := range want {
		if steps[i].StepID != want[i] {
			t.Fatalf("expected step %d to be %q, got %q", i, want[i], steps[i].StepID)
		}
	}
}

func assertTopologicalConstraints(t *testing.T, ordered []Step, deps map[string][]string) {
	t.Helper()
	indexByID := make(map[string]int, len(ordered))
	for i, step := range ordered {
		indexByID[step.StepID] = i
	}

	for stepID, depIDs := range deps {
		stepIndex, ok := indexByID[stepID]
		if !ok {
			t.Fatalf("expected step %q in ordered output", stepID)
		}
		for _, depID := range depIDs {
			depIndex, depOK := indexByID[depID]
			if !depOK {
				t.Fatalf("expected dependency %q in ordered output", depID)
			}
			if depIndex >= stepIndex {
				t.Fatalf("expected dependency %q before step %q (indexes %d >= %d)", depID, stepID, depIndex, stepIndex)
			}
		}
	}
}

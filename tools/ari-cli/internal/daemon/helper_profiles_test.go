package daemon

import "testing"

func TestDefaultHelperPromptsExcerptOneBaseContract(t *testing.T) {
	if helperPrompt() == "" {
		t.Fatalf("helper prompt is empty")
	}
}

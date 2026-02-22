package plan

import (
	"encoding/json"
	"testing"
)

func TestQuestionJSON_MarshalUnmarshal(t *testing.T) {
	original := Question{
		ID:      "q-1",
		Type:    QuestionTypeClarification,
		Prompt:  "What does done look like?",
		Context: "Feature request is ambiguous",
		Options: []string{"Option A", "Option B"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("expected marshal to succeed, got %v", err)
	}

	var decoded Question
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("expected unmarshal to succeed, got %v", err)
	}

	if decoded.ID != original.ID {
		t.Fatalf("expected id %q, got %q", original.ID, decoded.ID)
	}
	if decoded.Type != original.Type {
		t.Fatalf("expected type %q, got %q", original.Type, decoded.Type)
	}
	if decoded.Prompt != original.Prompt {
		t.Fatalf("expected prompt %q, got %q", original.Prompt, decoded.Prompt)
	}
	if decoded.Context != original.Context {
		t.Fatalf("expected context %q, got %q", original.Context, decoded.Context)
	}
	if len(decoded.Options) != len(original.Options) {
		t.Fatalf("expected %d options, got %d", len(original.Options), len(decoded.Options))
	}
	for i := range original.Options {
		if decoded.Options[i] != original.Options[i] {
			t.Fatalf("expected option %d to be %q, got %q", i, original.Options[i], decoded.Options[i])
		}
	}
}

func TestQuestionJSON_OmitsEmptyOptionalFields(t *testing.T) {
	q := Question{
		ID:     "q-2",
		Type:   QuestionTypeScope,
		Prompt: "Is this in scope?",
	}

	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("expected marshal to succeed, got %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("expected unmarshal to succeed, got %v", err)
	}

	if _, exists := raw["context"]; exists {
		t.Fatal("expected context to be omitted")
	}
	if _, exists := raw["options"]; exists {
		t.Fatal("expected options to be omitted")
	}
}

func TestAnswerJSON_MarshalUnmarshal(t *testing.T) {
	original := Answer{
		QuestionID: "q-3",
		Type:       ResponseTypeAnswer,
		Content:    "Ship only the core command for now",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("expected marshal to succeed, got %v", err)
	}

	var decoded Answer
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("expected unmarshal to succeed, got %v", err)
	}

	if decoded.QuestionID != original.QuestionID {
		t.Fatalf("expected question_id %q, got %q", original.QuestionID, decoded.QuestionID)
	}
	if decoded.Type != original.Type {
		t.Fatalf("expected type %q, got %q", original.Type, decoded.Type)
	}
	if decoded.Content != original.Content {
		t.Fatalf("expected content %q, got %q", original.Content, decoded.Content)
	}
}

func TestAnswerJSON_OmitsEmptyContent(t *testing.T) {
	a := Answer{
		QuestionID: "q-4",
		Type:       ResponseTypeSkip,
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("expected marshal to succeed, got %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("expected unmarshal to succeed, got %v", err)
	}

	if _, exists := raw["content"]; exists {
		t.Fatal("expected content to be omitted")
	}
}

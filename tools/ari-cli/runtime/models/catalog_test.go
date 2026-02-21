package models

import (
	"errors"
	"testing"
)

func TestParseCatalogDataEnvelopeNormalizesDeterministically(t *testing.T) {
	raw := []byte(`{
		"data": [
			{
				"id": "zai/model-z",
				"provider": " zai ",
				"name": "Z",
				"context_window": 4096,
				"input_modalities": [" text", "text", ""],
				"output_modalities": ["image", "text", "image"]
			},
			{
				"id": "openai/gpt-4o-mini",
				"name": "mini",
				"context_length": 8192,
				"input_modalities": ["audio", "text"],
				"output_modalities": ["text"]
			},
			{
				"id": "anthropic/claude-sonnet",
				"provider": "anthropic"
			}
		]
	}`)

	catalog, err := ParseCatalog(raw)
	if err != nil {
		t.Fatalf("ParseCatalog returned error: %v", err)
	}

	if len(catalog.Models) != 3 {
		t.Fatalf("model count = %d, want 3", len(catalog.Models))
	}

	if catalog.Models[0].ID != "anthropic/claude-sonnet" {
		t.Fatalf("model[0].ID = %q, want anthropic/claude-sonnet", catalog.Models[0].ID)
	}
	if catalog.Models[1].ID != "openai/gpt-4o-mini" {
		t.Fatalf("model[1].ID = %q, want openai/gpt-4o-mini", catalog.Models[1].ID)
	}
	if catalog.Models[2].ID != "zai/model-z" {
		t.Fatalf("model[2].ID = %q, want zai/model-z", catalog.Models[2].ID)
	}

	if catalog.Models[1].Provider != "openai" {
		t.Fatalf("derived provider = %q, want openai", catalog.Models[1].Provider)
	}
	if catalog.Models[1].ContextWindow != 8192 {
		t.Fatalf("context window = %d, want 8192", catalog.Models[1].ContextWindow)
	}
	if join(catalog.Models[2].InputModalities) != "text" {
		t.Fatalf("input modalities = %q, want text", join(catalog.Models[2].InputModalities))
	}
	if join(catalog.Models[2].OutputModalities) != "image,text" {
		t.Fatalf("output modalities = %q, want image,text", join(catalog.Models[2].OutputModalities))
	}
}

func TestParseCatalogArrayShape(t *testing.T) {
	raw := []byte(`[
		{"id":"openai/gpt-4.1","provider":"openai"},
		{"id":"anthropic/claude-3-7-sonnet","provider":"anthropic"}
	]`)

	catalog, err := ParseCatalog(raw)
	if err != nil {
		t.Fatalf("ParseCatalog returned error: %v", err)
	}

	if len(catalog.Models) != 2 {
		t.Fatalf("model count = %d, want 2", len(catalog.Models))
	}
}

func TestParseCatalogMalformedJSON(t *testing.T) {
	_, err := ParseCatalog([]byte(`{"data":`))
	if err == nil {
		t.Fatal("ParseCatalog returned nil error for malformed JSON")
	}
	if !errors.Is(err, ErrMalformedJSON) {
		t.Fatalf("error = %v, want ErrMalformedJSON", err)
	}
}

func TestParseCatalogMissingRequiredFields(t *testing.T) {
	_, err := ParseCatalog([]byte(`{"models":[{"provider":"openai"}]}`))
	if err == nil {
		t.Fatal("ParseCatalog returned nil error for missing id")
	}
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Fatalf("error = %v, want ErrMissingRequiredField", err)
	}
	if err.Error() != "missing required field: models[0].id" {
		t.Fatalf("error = %q, want %q", err.Error(), "missing required field: models[0].id")
	}

	_, err = ParseCatalog([]byte(`{"models":[{"id":"gpt-4o"}]}`))
	if err == nil {
		t.Fatal("ParseCatalog returned nil error for missing provider")
	}
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Fatalf("error = %v, want ErrMissingRequiredField", err)
	}
	if err.Error() != "missing required field: models[0].provider" {
		t.Fatalf("error = %q, want %q", err.Error(), "missing required field: models[0].provider")
	}
}

func join(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for i := 1; i < len(values); i++ {
		out += "," + values[i]
	}
	return out
}

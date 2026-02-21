package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

var (
	ErrMalformedJSON        = errors.New("malformed JSON")
	ErrMissingRequiredField = errors.New("missing required field")
)

type Catalog struct {
	Models []Model
}

type Model struct {
	ID               string   `json:"id"`
	Provider         string   `json:"provider"`
	Name             string   `json:"name,omitempty"`
	ContextWindow    int      `json:"context_window,omitempty"`
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
}

type rawModel struct {
	ID               string   `json:"id"`
	Provider         string   `json:"provider"`
	Name             string   `json:"name"`
	ContextWindow    int      `json:"context_window"`
	ContextLength    int      `json:"context_length"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

type rawCatalogEnvelope struct {
	Models []rawModel `json:"models"`
	Data   []rawModel `json:"data"`
}

func LoadCatalog(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}

	return ParseCatalog(data)
}

func ParseCatalog(data []byte) (Catalog, error) {
	entries, err := parseRawModels(data)
	if err != nil {
		return Catalog{}, err
	}

	models := make([]Model, 0, len(entries))
	for i, entry := range entries {
		model, err := normalizeModel(i, entry)
		if err != nil {
			return Catalog{}, err
		}
		models = append(models, model)
	}

	sort.Slice(models, func(i, j int) bool {
		leftProvider := canonicalName(models[i].Provider)
		rightProvider := canonicalName(models[j].Provider)
		if leftProvider != rightProvider {
			return leftProvider < rightProvider
		}

		leftID := canonicalName(models[i].ID)
		rightID := canonicalName(models[j].ID)
		if leftID != rightID {
			return leftID < rightID
		}

		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}

		return models[i].ID < models[j].ID
	})

	return Catalog{Models: models}, nil
}

func parseRawModels(data []byte) ([]rawModel, error) {
	var asList []rawModel
	if err := json.Unmarshal(data, &asList); err == nil {
		return asList, nil
	}

	var envelope rawCatalogEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedJSON, err)
	}

	if len(envelope.Models) > 0 {
		return envelope.Models, nil
	}
	if len(envelope.Data) > 0 {
		return envelope.Data, nil
	}

	return nil, fmt.Errorf("%w: models", ErrMissingRequiredField)
}

func normalizeModel(index int, entry rawModel) (Model, error) {
	id := strings.TrimSpace(entry.ID)
	if id == "" {
		return Model{}, fmt.Errorf("%w: models[%d].id", ErrMissingRequiredField, index)
	}

	provider := strings.TrimSpace(entry.Provider)
	if provider == "" {
		provider = providerFromID(id)
	}
	if provider == "" {
		return Model{}, fmt.Errorf("%w: models[%d].provider", ErrMissingRequiredField, index)
	}

	contextWindow := entry.ContextWindow
	if contextWindow == 0 {
		contextWindow = entry.ContextLength
	}

	return Model{
		ID:               id,
		Provider:         provider,
		Name:             strings.TrimSpace(entry.Name),
		ContextWindow:    contextWindow,
		InputModalities:  normalizeModalities(entry.InputModalities),
		OutputModalities: normalizeModalities(entry.OutputModalities),
	}, nil
}

func providerFromID(id string) string {
	parts := strings.SplitN(strings.TrimSpace(id), "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func normalizeModalities(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		set[trimmed] = struct{}{}
	}

	if len(set) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(set))
	for value := range set {
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)

	return normalized
}

func canonicalName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

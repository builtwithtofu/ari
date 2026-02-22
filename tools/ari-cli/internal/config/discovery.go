package config

import (
	"encoding/json"
)

type SchemaInfo struct {
	Path        string       `json:"path"`
	Type        string       `json:"type"`
	Description string       `json:"description"`
	Default     interface{}  `json:"default,omitempty"`
	Enum        []string     `json:"enum,omitempty"`
	Children    []SchemaInfo `json:"children,omitempty"`
}

func Discovery() (*SchemaInfo, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return nil, err
	}

	return parseSchema(schema, ""), nil
}

func parseSchema(schema map[string]interface{}, path string) *SchemaInfo {
	info := &SchemaInfo{Path: path}

	if t, ok := schema["type"].(string); ok {
		info.Type = t
	}
	if desc, ok := schema["description"].(string); ok {
		info.Description = desc
	}
	if def, ok := schema["default"]; ok {
		info.Default = def
	}
	if enum, ok := schema["enum"].([]interface{}); ok {
		for _, e := range enum {
			if s, ok := e.(string); ok {
				info.Enum = append(info.Enum, s)
			}
		}
	}

	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for key, val := range props {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if childSchema, ok := val.(map[string]interface{}); ok {
				child := parseSchema(childSchema, childPath)
				info.Children = append(info.Children, *child)
			}
		}
	}

	return info
}

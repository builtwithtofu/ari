package config

import (
	_ "embed"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed config.schema.json
var schemaJSON string

func Schema() (*gojsonschema.Schema, error) {
	loader := gojsonschema.NewStringLoader(schemaJSON)
	return gojsonschema.NewSchema(loader)
}

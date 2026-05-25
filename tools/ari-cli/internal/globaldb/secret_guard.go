package globaldb

import (
	"encoding/json"
	"strings"
)

var secretLikeFieldNames = map[string]struct{}{
	"access_token":          {},
	"auth_token":            {},
	"bearer_token":          {},
	"api_key":               {},
	"client_secret":         {},
	"credential":            {},
	"credential_source":     {},
	"credential_source_ref": {},
	"encryption_key":        {},
	"jwt":                   {},
	"password":              {},
	"pat":                   {},
	"private_key":           {},
	"refresh_token":         {},
	"secret":                {},
	"session_key":           {},
	"signing_key":           {},
	"source":                {},
	"source_ref":            {},
	"ssh_key":               {},
	"token":                 {},
}

func jsonContainsSecretLikeFields(raw string) bool {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return true
	}
	return valueContainsSecretLikeFields(value)
}

func valueContainsSecretLikeFields(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if isSecretLikeFieldName(key) || valueContainsSecretLikeFields(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if valueContainsSecretLikeFields(child) {
				return true
			}
		}
	}
	return false
}

func isSecretLikeFieldName(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	_, ok := secretLikeFieldNames[normalized]
	return ok
}

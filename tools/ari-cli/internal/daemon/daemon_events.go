package daemon

import "encoding/json"

// daemonEventPayload marshals small string payload maps for workspace event
// rows; on marshal failure it degrades to an empty object rather than
// blocking the fact from being recorded.
func daemonEventPayload(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

package daemon

import (
	"sort"
	"strings"
)

type harnessDefinition struct {
	resumableFlag string
}

var harnessDefinitions = map[string]harnessDefinition{
	"claude-code": {
		resumableFlag: "--resume",
	},
	"codex": {},
	"opencode": {
		resumableFlag: "--session",
	},
}

func resumableFlagForHarness(harness string) string {
	definition, ok := harnessDefinitions[strings.TrimSpace(harness)]
	if !ok {
		return ""
	}
	return definition.resumableFlag
}

func SupportedHarnesses() []string {
	names := make([]string, 0, len(harnessDefinitions))
	for name := range harnessDefinitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ResumableFlagForHarness(harness string) string {
	return resumableFlagForHarness(harness)
}

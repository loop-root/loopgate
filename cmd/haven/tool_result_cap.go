package main

import (
	"fmt"
	"strings"
)

// defaultToolResultMaxRunes caps model-facing tool output when no per-capability override applies.
const defaultToolResultMaxRunes = 20000

var toolResultMaxRunesByCapability = map[string]int{
	"fs_read":            16000,
	"fs_list":            12000,
	"shell_exec":         12000,
	"host.folder.list":   10000,
	"host.folder.read":   16000,
	"host.organize.plan": 20000,
	"host.plan.apply":    20000,
}

func maxToolResultRunesForCapability(capabilityName string) int {
	key := strings.TrimSpace(capabilityName)
	if key == "" {
		return defaultToolResultMaxRunes
	}
	if n, ok := toolResultMaxRunesByCapability[key]; ok && n > 0 {
		return n
	}
	return defaultToolResultMaxRunes
}

// capToolResultContentForModel truncates tool output shown to the model so repeated
// tool rounds do not explode Anthropic/OpenAI input TPM. Full fidelity remains in
// Loopgate/thread orchestration summaries where applicable.
func capToolResultContentForModel(capabilityName, content string) string {
	if content == "" {
		return content
	}
	maxRunes := maxToolResultRunesForCapability(capabilityName)
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	truncated := string(runes[:maxRunes])
	return truncated + fmt.Sprintf(
		"\n\n[Haven truncated tool output to %d Unicode code points for capability %q; use narrower reads or paging if you need the rest.]",
		maxRunes,
		capabilityName,
	)
}

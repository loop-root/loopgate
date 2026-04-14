package loopgate

import (
	"fmt"
	"strings"
)

func (server *Server) havenWakeStateSummaryText(tenantID string) (string, error) {
	wakeState, err := server.loadMemoryWakeState(tenantID)
	if err != nil {
		return "", err
	}
	if len(wakeState.RecentFacts) == 0 {
		return "", nil
	}

	var summaryBuilder strings.Builder

	var factLines []string
	for _, recentFact := range wakeState.RecentFacts {
		if len(factLines) >= 12 {
			break
		}
		factLines = append(factLines, fmt.Sprintf("  %s: %v", recentFact.Name, recentFact.Value))
	}
	if len(factLines) > 0 {
		summaryBuilder.WriteString("REMEMBERED CONTINUITY (authoritative):\n")
		summaryBuilder.WriteString(strings.Join(factLines, "\n"))
	} else {
		summaryBuilder.WriteString("Wake-state is loaded with continuity metadata.")
	}

	return summaryBuilder.String(), nil
}

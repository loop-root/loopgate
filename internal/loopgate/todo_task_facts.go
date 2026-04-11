package loopgate

import (
	"strings"

	tclpkg "morph/internal/tcl"
)

func (server *Server) buildExplicitTodoTaskFacts(itemID string, validatedRequest TodoAddRequest) []continuityDistillateFact {
	sourceRef := explicitTodoSourceKind + ":" + itemID
	taskFacts := []continuityDistillateFact{
		server.newExplicitTodoTaskFact(sourceRef, taskFactKind, validatedRequest.TaskKind),
		server.newExplicitTodoTaskFact(sourceRef, taskFactSourceKind, validatedRequest.SourceKind),
		server.newExplicitTodoTaskFact(sourceRef, taskFactExecutionClass, validatedRequest.ExecutionClass),
	}
	if validatedRequest.NextStep != "" {
		taskFacts = append(taskFacts, server.newExplicitTodoTaskFact(sourceRef, taskFactNextStep, validatedRequest.NextStep))
	}
	if validatedRequest.ScheduledForUTC != "" {
		taskFacts = append(taskFacts, server.newExplicitTodoTaskFact(sourceRef, taskFactScheduledForUT, validatedRequest.ScheduledForUTC))
	}
	return taskFacts
}

func (server *Server) newExplicitTodoTaskFact(sourceRef string, factName string, factValue string) continuityDistillateFact {
	normalizedFactValue := strings.TrimSpace(factValue)
	return continuityDistillateFact{
		Name:               factName,
		Value:              normalizedFactValue,
		SourceRef:          sourceRef,
		EpistemicFlavor:    "freshly_checked",
		SemanticProjection: deriveExplicitTodoTaskFactSemanticProjection(factName, normalizedFactValue),
	}
}

func deriveExplicitTodoTaskFactSemanticProjection(factName string, factValue string) *tclpkg.SemanticProjection {
	normalizedFactName := strings.TrimSpace(factName)
	normalizedFactValue := strings.TrimSpace(factValue)
	if normalizedFactName == "" || normalizedFactValue == "" {
		return nil
	}
	return deriveMemoryCandidateSemanticProjection(tclpkg.MemoryCandidate{
		Source:              tclpkg.CandidateSourceTaskMetadata,
		SourceChannel:       memorySourceChannelCapability,
		NormalizedFactKey:   normalizedFactName,
		NormalizedFactValue: normalizedFactValue,
		Trust:               tclpkg.TrustSystemDerived,
		Actor:               tclpkg.ObjectSystem,
	})
}

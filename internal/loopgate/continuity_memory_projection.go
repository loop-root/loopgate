package loopgate

func ptrContinuityInspectionRecord(inspectionRecord continuityInspectionRecord) *continuityInspectionRecord {
	return &inspectionRecord
}

func ptrContinuityDistillateRecord(distillateRecord continuityDistillateRecord) *continuityDistillateRecord {
	return &distillateRecord
}

func ptrContinuityResonateKeyRecord(resonateKeyRecord continuityResonateKeyRecord) *continuityResonateKeyRecord {
	return &resonateKeyRecord
}

func ptrContinuityInspectionReview(review continuityInspectionReview) *continuityInspectionReview {
	return &review
}

func ptrContinuityInspectionLineage(lineage continuityInspectionLineage) *continuityInspectionLineage {
	return &lineage
}

func ptrContinuityObservedPacket(observedPacket continuityObservedPacket) *continuityObservedPacket {
	return &observedPacket
}

func cloneContinuityObservedPacket(observedPacket continuityObservedPacket) continuityObservedPacket {
	observedPacket.Tags = append([]string(nil), observedPacket.Tags...)
	observedPacket.Events = append([]continuityObservedEventRecord(nil), observedPacket.Events...)
	for eventIndex := range observedPacket.Events {
		observedPacket.Events[eventIndex] = cloneContinuityObservedEventRecord(observedPacket.Events[eventIndex])
	}
	return observedPacket
}

func cloneContinuityObservedEventRecord(observedEvent continuityObservedEventRecord) continuityObservedEventRecord {
	observedEvent.SourceRefs = append([]continuityArtifactSourceRef(nil), observedEvent.SourceRefs...)
	if observedEvent.Payload != nil {
		clonedPayload := *observedEvent.Payload
		clonedPayload.Facts = append([]continuityObservedFactRecord(nil), observedEvent.Payload.Facts...)
		observedEvent.Payload = &clonedPayload
	}
	return observedEvent
}

func memoryDiagnosticWakeResponseFromReport(diagnosticReport continuityDiagnosticWakeReport) MemoryDiagnosticWakeResponse {
	response := MemoryDiagnosticWakeResponse{
		SchemaVersion:     diagnosticReport.SchemaVersion,
		ResolutionVersion: diagnosticReport.ResolutionVersion,
		ReportID:          diagnosticReport.ReportID,
		CreatedAtUTC:      diagnosticReport.CreatedAtUTC,
		RuntimeWakeID:     diagnosticReport.RuntimeWakeID,
		IncludedCount:     len(diagnosticReport.Entries),
		ExcludedCount:     len(diagnosticReport.ExcludedEntries),
		Entries:           make([]MemoryDiagnosticWakeEntry, 0, len(diagnosticReport.Entries)),
		ExcludedEntries:   make([]MemoryDiagnosticWakeEntry, 0, len(diagnosticReport.ExcludedEntries)),
	}
	for _, reportEntry := range diagnosticReport.Entries {
		response.Entries = append(response.Entries, memoryDiagnosticWakeEntryFromContinuity(reportEntry))
	}
	for _, reportEntry := range diagnosticReport.ExcludedEntries {
		response.ExcludedEntries = append(response.ExcludedEntries, memoryDiagnosticWakeEntryFromContinuity(reportEntry))
	}
	return response
}

func memoryDiagnosticWakeEntryFromContinuity(reportEntry continuityDiagnosticWakeEntry) MemoryDiagnosticWakeEntry {
	return MemoryDiagnosticWakeEntry{
		ItemKind:         reportEntry.ItemKind,
		GoalFamilyID:     reportEntry.GoalFamilyID,
		Scope:            reportEntry.Scope,
		RetentionScore:   reportEntry.RetentionScore,
		EffectiveHotness: reportEntry.EffectiveHotness,
		Reason:           reportEntry.Reason,
		TrimReason:       reportEntry.TrimReason,
		PrecedenceSource: reportEntry.PrecedenceSource,
		ScoreTrace:       append([]string(nil), reportEntry.ScoreTrace...),
		RedactedSummary:  reportEntry.RedactedSummary,
	}
}

func cloneMemoryDiagnosticWakeResponse(diagnosticResponse MemoryDiagnosticWakeResponse) MemoryDiagnosticWakeResponse {
	diagnosticResponse.Entries = append([]MemoryDiagnosticWakeEntry(nil), diagnosticResponse.Entries...)
	diagnosticResponse.ExcludedEntries = append([]MemoryDiagnosticWakeEntry(nil), diagnosticResponse.ExcludedEntries...)
	for entryIndex := range diagnosticResponse.Entries {
		diagnosticResponse.Entries[entryIndex].ScoreTrace = append([]string(nil), diagnosticResponse.Entries[entryIndex].ScoreTrace...)
	}
	for entryIndex := range diagnosticResponse.ExcludedEntries {
		diagnosticResponse.ExcludedEntries[entryIndex].ScoreTrace = append([]string(nil), diagnosticResponse.ExcludedEntries[entryIndex].ScoreTrace...)
	}
	return diagnosticResponse
}

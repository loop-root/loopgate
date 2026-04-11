package loopgate

import (
	"context"
	"fmt"
	"time"
)

func (server *Server) mutateContinuityMemory(tenantID string, controlSessionID string, auditEventType string, applyMutation func(continuityMemoryState, time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error)) error {
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()

	partition, err := server.ensureMemoryPartitionLocked(tenantID)
	if err != nil {
		return err
	}

	workingState := cloneContinuityMemoryState(partition.state)
	nowUTC := server.now().UTC()

	updatedState, auditData, mutationEvents, err := applyMutation(workingState, nowUTC)
	if err != nil {
		return err
	}
	updatedState.WakeState, updatedState.DiagnosticWake = buildLoopgateWakeProducts(updatedState, nowUTC, server.runtimeConfig)

	memoryPaths := newContinuityMemoryPaths(partition.rootPath, legacyContinuityPathForPartitionFromKey(server, partition.partitionKey))
	// Security ordering: append durable continuity JSONL only after the hash-chained audit
	// ledger records the mutation. Otherwise a failed audit leaves replayable continuity
	// events with no corresponding audit evidence (reviewers: CR S1, GR Finding 2, MR F10–F12).
	if auditData != nil {
		if err := server.logEvent(auditEventType, controlSessionID, auditData); err != nil {
			return err
		}
	}
	if err := appendContinuityMutationEvents(memoryPaths, mutationEvents); err != nil {
		return err
	}
	if err := server.saveMemoryState(partition.rootPath, updatedState, server.runtimeConfig); err != nil {
		return err
	}
	partition.state = canonicalizeContinuityMemoryState(updatedState)
	if partition.backend != nil {
		if syncErr := partition.backend.SyncAuthoritativeState(context.Background(), partition.state); syncErr != nil {
			return syncErr
		}
	}
	return nil
}

func (server *Server) consumeMemoryFactWriteBudgetLocked(controlSessionID string, peerUID uint32, nowUTC time.Time) error {
	windowSeconds := server.runtimeConfig.Memory.ExplicitFactWrites.WindowSeconds
	if windowSeconds <= 0 {
		windowSeconds = 60
	}
	windowStartUTC := nowUTC.Add(-time.Duration(windowSeconds) * time.Second)
	sessionWrites := pruneOldMemoryFactWrites(server.memoryFactWritesBySession[controlSessionID], windowStartUTC)
	peerWrites := pruneOldMemoryFactWrites(server.memoryFactWritesByUID[peerUID], windowStartUTC)

	if len(sessionWrites) >= server.runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerSession {
		server.memoryFactWritesBySession[controlSessionID] = sessionWrites
		server.memoryFactWritesByUID[peerUID] = peerWrites
		return continuityGovernanceError{
			httpStatus:     429,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeMemoryFactWriteRateLimited,
			reason:         fmt.Sprintf("explicit memory fact write rate limit exceeded for control session; retry after %ds", windowSeconds),
		}
	}
	if len(peerWrites) >= server.runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerPeerUID {
		server.memoryFactWritesBySession[controlSessionID] = sessionWrites
		server.memoryFactWritesByUID[peerUID] = peerWrites
		return continuityGovernanceError{
			httpStatus:     429,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeMemoryFactWriteRateLimited,
			reason:         fmt.Sprintf("explicit memory fact write rate limit exceeded for local peer identity; retry after %ds", windowSeconds),
		}
	}

	sessionWrites = append(sessionWrites, nowUTC)
	peerWrites = append(peerWrites, nowUTC)
	server.memoryFactWritesBySession[controlSessionID] = sessionWrites
	server.memoryFactWritesByUID[peerUID] = peerWrites
	return nil
}

func pruneOldMemoryFactWrites(writeTimes []time.Time, windowStartUTC time.Time) []time.Time {
	if len(writeTimes) == 0 {
		return nil
	}
	filteredWriteTimes := writeTimes[:0]
	for _, writeTime := range writeTimes {
		if writeTime.Before(windowStartUTC) {
			continue
		}
		filteredWriteTimes = append(filteredWriteTimes, writeTime)
	}
	return filteredWriteTimes
}

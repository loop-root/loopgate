package loopgate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// memoryPartitionsDirName is the subdirectory under memoryBasePath holding per-tenant
// continuity trees. Raw tenant strings never appear in paths — only stable hashed
// directory names (or "default" for empty deployment tenant / personal mode).
const memoryPartitionsDirName = "partitions"

// memoryPartition holds authoritative continuity JSON/JSONL, derived artifacts, and
// the SQLite projection for one tenant namespace. Cross-partition access is denied
// by routing all memory I/O through the session's TenantID.
type memoryPartition struct {
	partitionKey string
	tenantID     string
	rootPath     string
	state        continuityMemoryState
	backend      MemoryBackend
}

func memoryPartitionKey(rawTenantID string) string {
	trimmed := strings.TrimSpace(rawTenantID)
	if trimmed == "" {
		return "default"
	}
	sum := sha256.Sum256([]byte(trimmed))
	return fmt.Sprintf("t%x", sum[:16])
}

// maybeMigrateMemoryToPartitionedLayout moves a pre-partition on-disk layout
// (artifacts directly under memoryBasePath) into memory/partitions/default/.
// If partitions/ already exists, this is a no-op so repeat calls are safe.
func maybeMigrateMemoryToPartitionedLayout(memoryBasePath string) error {
	if err := os.MkdirAll(memoryBasePath, 0o700); err != nil {
		return fmt.Errorf("ensure memory base path: %w", err)
	}
	partitionsDir := filepath.Join(memoryBasePath, memoryPartitionsDirName)
	if fi, err := os.Stat(partitionsDir); err == nil && fi.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(memoryBasePath)
	if err != nil {
		return fmt.Errorf("read memory base path: %w", err)
	}
	var topLevelNames []string
	for _, entry := range entries {
		name := entry.Name()
		if name == memoryPartitionsDirName {
			continue
		}
		topLevelNames = append(topLevelNames, name)
	}
	defaultDir := filepath.Join(partitionsDir, memoryPartitionKey(""))
	if len(topLevelNames) == 0 {
		return os.MkdirAll(defaultDir, 0o700)
	}
	if err := os.MkdirAll(defaultDir, 0o700); err != nil {
		return fmt.Errorf("create default memory partition dir: %w", err)
	}
	for _, name := range topLevelNames {
		from := filepath.Join(memoryBasePath, name)
		to := filepath.Join(defaultDir, name)
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("migrate memory artifact %q into default partition: %w", name, err)
		}
	}
	return nil
}

// ensureMemoryPartitionLocked returns the partition for rawTenantID, creating and
// loading state on first use. Caller must hold server.memoryMu.
func (server *Server) ensureMemoryPartitionLocked(rawTenantID string) (*memoryPartition, error) {
	tenantID := strings.TrimSpace(rawTenantID)
	key := memoryPartitionKey(tenantID)
	if existing := server.memoryPartitions[key]; existing != nil {
		return existing, nil
	}
	rootPath := filepath.Join(server.memoryBasePath, memoryPartitionsDirName, key)
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		return nil, fmt.Errorf("ensure memory partition dir: %w", err)
	}
	loadedState, err := loadContinuityMemoryState(rootPath, legacyContinuityPathForPartitionFromKey(server, key))
	if err != nil {
		wrappedLoadErr := fmt.Errorf("load continuity memory partition %q at %q: %w", key, rootPath, err)
		// Partition replay/load failures are security-relevant because Loopgate must
		// fail closed instead of silently continuing from ambiguous authoritative state.
		if server.reportSecurityWarning != nil {
			server.reportSecurityWarning("continuity_partition_load_failed", wrappedLoadErr)
		}
		return nil, wrappedLoadErr
	}
	partition := &memoryPartition{
		partitionKey: key,
		tenantID:     tenantID,
		rootPath:     rootPath,
		state:        loadedState,
	}
	backend, err := newMemoryBackendForPartition(server, partition)
	if err != nil {
		return nil, err
	}
	partition.backend = backend
	server.memoryPartitions[key] = partition
	return partition, nil
}

func legacyContinuityPathForPartitionFromKey(server *Server, partitionKey string) string {
	if partitionKey == memoryPartitionKey("") {
		return server.memoryLegacyPath
	}
	return ""
}

func (server *Server) initDefaultMemoryPartitionLocked() error {
	_, err := server.ensureMemoryPartitionLocked("")
	return err
}

func (server *Server) rebuildContinuityWakeStateFromAuthority() error {
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()
	for _, partition := range server.memoryPartitions {
		normalizedState := cloneContinuityMemoryState(partition.state)
		for inspectionID, inspectionRecord := range normalizedState.Inspections {
			normalizedState.Inspections[inspectionID] = normalizeContinuityInspectionRecordMust(inspectionRecord)
		}
		normalizedState.WakeState, normalizedState.DiagnosticWake = buildLoopgateWakeProducts(normalizedState, server.now().UTC(), server.runtimeConfig)
		if continuityMemoryStatesEqual(partition.state, normalizedState) {
			partition.state = normalizedState
			continue
		}
		if err := server.saveMemoryState(partition.rootPath, normalizedState, server.runtimeConfig); err != nil {
			return err
		}
		partition.state = normalizedState
	}
	return nil
}

func (server *Server) syncMemoryBackendFromAuthority() error {
	server.memoryMu.Lock()
	type partitionSyncWork struct {
		partition *memoryPartition
		state     continuityMemoryState
	}
	syncWork := make([]partitionSyncWork, 0, len(server.memoryPartitions))
	for _, partition := range server.memoryPartitions {
		syncWork = append(syncWork, partitionSyncWork{
			partition: partition,
			state:     cloneContinuityMemoryState(partition.state),
		})
	}
	server.memoryMu.Unlock()
	for _, work := range syncWork {
		if work.partition.backend == nil {
			return fmt.Errorf("memory backend is not configured for partition %q", work.partition.partitionKey)
		}
		if err := work.partition.backend.SyncAuthoritativeState(context.Background(), work.state); err != nil {
			return err
		}
	}
	return nil
}

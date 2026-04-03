package loopgate

import (
	"os"
	"path/filepath"
	"testing"
)

func testDefaultPartitionRoot(t *testing.T, server *Server) string {
	t.Helper()
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()
	partition, ok := server.memoryPartitions[memoryPartitionKey("")]
	if !ok || partition == nil {
		t.Fatal("missing default memory partition")
	}
	return partition.rootPath
}

func testDefaultMemoryState(t *testing.T, server *Server) continuityMemoryState {
	t.Helper()
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()
	partition := server.memoryPartitions[memoryPartitionKey("")]
	if partition == nil {
		t.Fatal("missing default memory partition")
	}
	return partition.state
}

func testSetDefaultMemoryState(t *testing.T, server *Server, state continuityMemoryState) {
	t.Helper()
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()
	partition := server.memoryPartitions[memoryPartitionKey("")]
	if partition == nil {
		t.Fatal("missing default memory partition")
	}
	partition.state = canonicalizeContinuityMemoryState(state)
}

func defaultContinuityTCLBackendForTests(t *testing.T, server *Server) *continuityTCLMemoryBackend {
	t.Helper()
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()
	partition, ok := server.memoryPartitions[memoryPartitionKey("")]
	if !ok || partition == nil {
		t.Fatal("missing default memory partition")
	}
	backend, ok := partition.backend.(*continuityTCLMemoryBackend)
	if !ok {
		t.Fatalf("expected continuity_tcl backend, got %T", partition.backend)
	}
	return backend
}

func newTestServerWithStubMemoryBackend(t *testing.T, stub MemoryBackend) *Server {
	t.Helper()
	memBase := filepath.Join(t.TempDir(), "memory-base")
	if err := maybeMigrateMemoryToPartitionedLayout(memBase); err != nil {
		t.Fatalf("migrate memory layout: %v", err)
	}
	root := filepath.Join(memBase, memoryPartitionsDirName, memoryPartitionKey(""))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir partition root: %v", err)
	}
	return &Server{
		memoryBasePath:   memBase,
		memoryLegacyPath: filepath.Join(t.TempDir(), "loopgate_memory.json"),
		memoryPartitions: map[string]*memoryPartition{
			memoryPartitionKey(""): {
				partitionKey: memoryPartitionKey(""),
				tenantID:     "",
				rootPath:     root,
				state:        newEmptyContinuityMemoryState(),
				backend:      stub,
			},
		},
	}
}

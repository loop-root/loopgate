package loopgate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaybeMigrateMemoryToPartitionedLayout_EmptyBaseCreatesDefaultPartition(t *testing.T) {
	base := t.TempDir()
	if err := maybeMigrateMemoryToPartitionedLayout(base); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defaultDir := filepath.Join(base, memoryPartitionsDirName, memoryPartitionKey(""))
	fi, err := os.Stat(defaultDir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("expected default partition dir %q to exist, stat err=%v isDir=%v", defaultDir, err, fi != nil && fi.IsDir())
	}
	if err := maybeMigrateMemoryToPartitionedLayout(base); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMaybeMigrateMemoryToPartitionedLayout_MovesLegacyTopLevelArtifacts(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	marker := filepath.Join(base, "legacy_marker.txt")
	if err := os.WriteFile(marker, []byte("migrated"), 0o600); err != nil {
		t.Fatalf("write legacy marker: %v", err)
	}
	nestedDir := filepath.Join(base, "nested_dir")
	if err := os.MkdirAll(nestedDir, 0o700); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	nestedFile := filepath.Join(nestedDir, "inner.txt")
	if err := os.WriteFile(nestedFile, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	if err := maybeMigrateMemoryToPartitionedLayout(base); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	defaultDir := filepath.Join(base, memoryPartitionsDirName, memoryPartitionKey(""))
	if _, err := os.Stat(filepath.Join(defaultDir, "legacy_marker.txt")); err != nil {
		t.Fatalf("expected marker under default partition: %v", err)
	}
	if _, err := os.Stat(filepath.Join(defaultDir, "nested_dir", "inner.txt")); err != nil {
		t.Fatalf("expected nested tree under default partition: %v", err)
	}
	if entries, err := os.ReadDir(base); err != nil {
		t.Fatalf("readdir base: %v", err)
	} else {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if len(names) != 1 || names[0] != memoryPartitionsDirName {
			t.Fatalf("expected only %q at memory base after migrate, got %#v", memoryPartitionsDirName, names)
		}
	}
}

func TestMaybeMigrateMemoryToPartitionedLayout_ExistingPartitionsDirIsNoOp(t *testing.T) {
	base := t.TempDir()
	partitionsDir := filepath.Join(base, memoryPartitionsDirName)
	if err := os.MkdirAll(partitionsDir, 0o700); err != nil {
		t.Fatalf("mkdir partitions: %v", err)
	}
	orphanAtRoot := filepath.Join(base, "should_not_move.txt")
	if err := os.WriteFile(orphanAtRoot, []byte("left"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	if err := maybeMigrateMemoryToPartitionedLayout(base); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := os.Stat(orphanAtRoot); err != nil {
		t.Fatalf("expected top-level file to remain when partitions/ already exists: %v", err)
	}
}

func TestMemoryPartitionKey_EmptyVersusDistinctTenants(t *testing.T) {
	if memoryPartitionKey("") != "default" {
		t.Fatalf("empty tenant must map to default, got %q", memoryPartitionKey(""))
	}
	a := memoryPartitionKey("tenant-org-a")
	b := memoryPartitionKey("tenant-org-b")
	if a == b {
		t.Fatalf("expected distinct partition keys, both %q", a)
	}
	if a == "default" || b == "default" {
		t.Fatalf("non-empty tenants must not use default key, got a=%q b=%q", a, b)
	}
}

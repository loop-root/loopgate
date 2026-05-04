package loopgate

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func ageQuarantineRecordForPrune(t *testing.T, repoRoot string, quarantineRef string) {
	t.Helper()

	recordPath, err := quarantinePathFromRef(repoRoot, quarantineRef)
	if err != nil {
		t.Fatalf("quarantine path from ref: %v", err)
	}
	recordBytes, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read quarantine record: %v", err)
	}
	var sourceRecord quarantinedPayloadRecord
	if err := json.Unmarshal(recordBytes, &sourceRecord); err != nil {
		t.Fatalf("unmarshal quarantine record: %v", err)
	}
	sourceRecord.StoredAtUTC = time.Now().UTC().Add(-quarantineBlobRetentionPeriod - time.Hour).Format(time.RFC3339Nano)
	if err := writeQuarantinedPayloadRecord(recordPath, sourceRecord); err != nil {
		t.Fatalf("rewrite quarantine record: %v", err)
	}
}

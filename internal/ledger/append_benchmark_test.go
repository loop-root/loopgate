package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkAppendRuntimeLatency(b *testing.B) {
	b.Run("serial_fsync", func(b *testing.B) {
		benchmarkAppendRuntime(b, true, false)
	})
	b.Run("parallel_fsync", func(b *testing.B) {
		benchmarkAppendRuntime(b, true, true)
	})
	b.Run("serial_no_fsync", func(b *testing.B) {
		benchmarkAppendRuntime(b, false, false)
	})
}

func benchmarkAppendRuntime(b *testing.B, syncLedger bool, parallel bool) {
	b.Helper()
	b.ReportAllocs()

	ledgerPath := filepath.Join(b.TempDir(), "loopgate_events.jsonl")
	appendRuntime := NewAppendRuntime()
	if !syncLedger {
		appendRuntime.syncLedgerFileFunc = func(_ *os.File) error {
			return nil
		}
	}
	durations := make([]int64, b.N)

	b.ResetTimer()
	startedAll := time.Now()
	if parallel {
		var nextIndex uint64
		var failedAppends uint64
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				index := atomic.AddUint64(&nextIndex, 1) - 1
				startedAt := time.Now()
				if err := appendRuntime.Append(ledgerPath, benchmarkLedgerEvent(index)); err != nil {
					atomic.AddUint64(&failedAppends, 1)
				}
				durations[index] = time.Since(startedAt).Nanoseconds()
			}
		})
		if failedAppends > 0 {
			b.Fatalf("expected all ledger appends to succeed, got %d failures", failedAppends)
		}
	} else {
		for index := 0; index < b.N; index++ {
			startedAt := time.Now()
			if err := appendRuntime.Append(ledgerPath, benchmarkLedgerEvent(uint64(index))); err != nil {
				b.Fatalf("append benchmark ledger event: %v", err)
			}
			durations[index] = time.Since(startedAt).Nanoseconds()
		}
	}
	elapsed := time.Since(startedAll)
	b.StopTimer()

	reportBenchmarkLatencyPercentiles(b, durations)
	reportBenchmarkThroughput(b, b.N, elapsed)
}

func benchmarkLedgerEvent(index uint64) Event {
	return NewEvent(
		time.Date(2026, 5, 4, 12, 0, int(index%60), int(index%1_000_000_000), time.UTC).Format(time.RFC3339Nano),
		"benchmark.ledger_append",
		"benchmark-ledger-session",
		map[string]interface{}{
			"benchmark": "ledger_append",
			"index":     index,
			"message":   fmt.Sprintf("benchmark event %d", index),
		},
	)
}

func reportBenchmarkLatencyPercentiles(b *testing.B, durations []int64) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sortedDurations := append([]int64(nil), durations...)
	sort.Slice(sortedDurations, func(left, right int) bool {
		return sortedDurations[left] < sortedDurations[right]
	})
	b.ReportMetric(float64(benchmarkPercentileDuration(sortedDurations, 50))/float64(time.Microsecond), "p50_us")
	b.ReportMetric(float64(benchmarkPercentileDuration(sortedDurations, 95))/float64(time.Microsecond), "p95_us")
	b.ReportMetric(float64(benchmarkPercentileDuration(sortedDurations, 99))/float64(time.Microsecond), "p99_us")
}

func benchmarkPercentileDuration(sortedDurations []int64, percentile int) int64 {
	if len(sortedDurations) == 0 {
		return 0
	}
	if percentile <= 0 {
		return sortedDurations[0]
	}
	if percentile >= 100 {
		return sortedDurations[len(sortedDurations)-1]
	}
	index := ((len(sortedDurations) * percentile) + 99) / 100
	if index <= 0 {
		return sortedDurations[0]
	}
	return sortedDurations[index-1]
}

func reportBenchmarkThroughput(b *testing.B, operationCount int, elapsed time.Duration) {
	b.Helper()
	if operationCount <= 0 || elapsed <= 0 {
		return
	}
	b.ReportMetric(float64(operationCount)/elapsed.Seconds(), "ops_per_sec")
}

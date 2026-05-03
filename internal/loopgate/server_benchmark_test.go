package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"loopgate/internal/testutil"
)

func BenchmarkHealthRouteLatency(b *testing.B) {
	server := &Server{
		httpRequestSlots: make(chan struct{}, 256),
	}
	handler := server.wrapHTTPHandler(http.HandlerFunc(server.handleHealth))

	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()
		durations := make([]int64, b.N)
		var nextIndex uint64
		var failedRequests uint64

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				index := atomic.AddUint64(&nextIndex, 1) - 1
				request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
				recorder := httptest.NewRecorder()

				startedAt := time.Now()
				handler.ServeHTTP(recorder, request)
				durations[index] = time.Since(startedAt).Nanoseconds()

				if recorder.Code != http.StatusOK {
					atomic.AddUint64(&failedRequests, 1)
				}
			}
		})
		b.StopTimer()

		if failedRequests > 0 {
			b.Fatalf("expected all health requests to succeed, got %d failures", failedRequests)
		}
		reportLatencyPercentiles(b, durations)
	})
}

func BenchmarkHookPreValidateReadLatency(b *testing.B) {
	repoRoot := newLoopgateBenchmarkRepoRoot(b)
	server := newLoopgateBenchmarkServer(b, repoRoot)
	server.hookPreValidateRateLimit = 0
	handler := server.wrapHTTPHandler(http.HandlerFunc(server.handleHookPreValidate))
	requestBody := mustBenchmarkJSON(b, controlapipkg.HookPreValidateRequest{
		HookEventName: claudeCodeHookEventPreToolUse,
		ToolName:      "Read",
		ToolUseID:     "bench-read",
		CWD:           repoRoot,
		SessionID:     "bench-hook-session",
		ToolInput: map[string]interface{}{
			"file_path": "README.md",
		},
	})
	requestContext := context.WithValue(context.Background(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: os.Getpid(),
	})

	b.Run("parallel_audited", func(b *testing.B) {
		b.ReportAllocs()
		durations := make([]int64, b.N)
		var nextIndex uint64
		var failedRequests uint64

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				index := atomic.AddUint64(&nextIndex, 1) - 1
				request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", bytes.NewReader(requestBody))
				request = request.WithContext(requestContext)
				recorder := httptest.NewRecorder()

				startedAt := time.Now()
				handler.ServeHTTP(recorder, request)
				durations[index] = time.Since(startedAt).Nanoseconds()

				if recorder.Code != http.StatusOK {
					atomic.AddUint64(&failedRequests, 1)
				}
			}
		})
		b.StopTimer()

		if failedRequests > 0 {
			b.Fatalf("expected all hook requests to return HTTP 200, got %d failures", failedRequests)
		}
		reportLatencyPercentiles(b, durations)
	})
}

func BenchmarkAuditAppendLatency(b *testing.B) {
	repoRoot := newLoopgateBenchmarkRepoRoot(b)
	server := newLoopgateBenchmarkServer(b, repoRoot)

	b.Run("parallel_fsync", func(b *testing.B) {
		b.ReportAllocs()
		durations := make([]int64, b.N)
		var nextIndex uint64
		var failedAppends uint64

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				index := atomic.AddUint64(&nextIndex, 1) - 1
				startedAt := time.Now()
				err := server.logEvent("benchmark.audit_append", "bench-audit-session", map[string]interface{}{
					"benchmark": "audit_append",
				})
				durations[index] = time.Since(startedAt).Nanoseconds()
				if err != nil {
					atomic.AddUint64(&failedAppends, 1)
				}
			}
		})
		b.StopTimer()

		if failedAppends > 0 {
			b.Fatalf("expected all audit appends to succeed, got %d failures", failedAppends)
		}
		reportLatencyPercentiles(b, durations)
	})
}

func newLoopgateBenchmarkRepoRoot(b *testing.B) string {
	b.Helper()

	repoRoot := b.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("benchmark fixture\n"), 0o600); err != nil {
		b.Fatalf("write benchmark readme fixture: %v", err)
	}
	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		b.Fatalf("new benchmark policy signer: %v", err)
	}
	policySigner.ConfigureEnv(b.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, loopgatePolicyYAML(false)); err != nil {
		b.Fatalf("write benchmark signed policy: %v", err)
	}
	return repoRoot
}

func newLoopgateBenchmarkServer(b *testing.B, repoRoot string) *Server {
	b.Helper()

	server, err := NewServer(repoRoot, filepath.Join(b.TempDir(), "loopgate.sock"))
	if err != nil {
		b.Fatalf("new benchmark server: %v", err)
	}
	server.sessionOpenMinInterval = 0
	server.expirySweepMaxInterval = 0
	return server
}

func mustBenchmarkJSON(b *testing.B, value interface{}) []byte {
	b.Helper()

	encodedBytes, err := json.Marshal(value)
	if err != nil {
		b.Fatalf("marshal benchmark json: %v", err)
	}
	return encodedBytes
}

func reportLatencyPercentiles(b *testing.B, durations []int64) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sortedDurations := append([]int64(nil), durations...)
	sort.Slice(sortedDurations, func(left, right int) bool {
		return sortedDurations[left] < sortedDurations[right]
	})
	b.ReportMetric(float64(percentileDuration(sortedDurations, 50))/float64(time.Microsecond), "p50_us")
	b.ReportMetric(float64(percentileDuration(sortedDurations, 95))/float64(time.Microsecond), "p95_us")
	b.ReportMetric(float64(percentileDuration(sortedDurations, 99))/float64(time.Microsecond), "p99_us")
}

func percentileDuration(sortedDurations []int64, percentile int) int64 {
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

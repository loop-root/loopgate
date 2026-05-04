package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"loopgate/internal/ledger"
	controlapipkg "loopgate/internal/loopgate/controlapi"
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

func BenchmarkCapabilityExecutionLatency(b *testing.B) {
	b.Run("parallel_fs_read_audited", func(b *testing.B) {
		repoRoot := newLoopgateBenchmarkRepoRoot(b)
		server := newLoopgateBenchmarkServer(b, repoRoot)
		server.fsReadRateLimit = 0
		writeBenchmarkSandboxFile(b, server, "README.md", "benchmark fixture\n")
		tokenClaims := benchmarkCapabilityToken("bench-capability-session", []string{"fs_read"})
		b.ReportAllocs()
		durations := make([]int64, b.N)
		var nextIndex uint64
		var failedExecutions uint64
		var firstFailure atomic.Value

		b.ResetTimer()
		startedAll := time.Now()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				index := atomic.AddUint64(&nextIndex, 1) - 1
				capabilityRequest := controlapipkg.CapabilityRequest{
					RequestID:  benchmarkRequestID("req-benchmark-fs-read", index),
					Capability: "fs_read",
					Arguments: map[string]string{
						"path": "README.md",
					},
				}

				startedAt := time.Now()
				response := server.executeCapabilityRequest(context.Background(), tokenClaims, capabilityRequest, false)
				durations[index] = time.Since(startedAt).Nanoseconds()
				if response.Status != controlapipkg.ResponseStatusSuccess {
					if atomic.LoadUint64(&failedExecutions) == 0 {
						firstFailure.Store(fmt.Sprintf("%#v", response))
					}
					atomic.AddUint64(&failedExecutions, 1)
				}
			}
		})
		elapsed := time.Since(startedAll)
		b.StopTimer()

		if failedExecutions > 0 {
			b.Fatalf("expected all fs_read executions to succeed, got %d failures; first=%v", failedExecutions, firstFailure.Load())
		}
		reportLatencyPercentiles(b, durations)
		reportThroughput(b, b.N, elapsed)
	})
}

func BenchmarkApprovalCreationLatency(b *testing.B) {
	b.Run("parallel_fs_write_pending", func(b *testing.B) {
		repoRoot := newLoopgateBenchmarkRepoRootWithPolicy(b, loopgatePolicyYAML(true))
		server := newLoopgateBenchmarkServer(b, repoRoot)
		server.maxPendingApprovalsPerControlSession = b.N + 1
		server.maxTotalApprovalRecords = b.N + 1
		server.maxSeenRequestReplayEntries = b.N + 1
		tokenClaims := benchmarkCapabilityToken("bench-approval-session", []string{"fs_write"})
		b.ReportAllocs()
		durations := make([]int64, b.N)
		var nextIndex uint64
		var failedApprovals uint64

		b.ResetTimer()
		startedAll := time.Now()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				index := atomic.AddUint64(&nextIndex, 1) - 1
				capabilityRequest := controlapipkg.CapabilityRequest{
					RequestID:  benchmarkRequestID("req-benchmark-approval", index),
					Capability: "fs_write",
					Arguments: map[string]string{
						"path":    benchmarkRequestID("approval-output", index) + ".txt",
						"content": "approval benchmark payload",
					},
				}

				startedAt := time.Now()
				response := server.executeCapabilityRequest(context.Background(), tokenClaims, capabilityRequest, true)
				durations[index] = time.Since(startedAt).Nanoseconds()
				if response.Status != controlapipkg.ResponseStatusPendingApproval || !response.ApprovalRequired {
					atomic.AddUint64(&failedApprovals, 1)
				}
			}
		})
		elapsed := time.Since(startedAll)
		b.StopTimer()

		if failedApprovals > 0 {
			b.Fatalf("expected all fs_write requests to create pending approvals, got %d failures", failedApprovals)
		}
		reportLatencyPercentiles(b, durations)
		reportThroughput(b, b.N, elapsed)
	})
}

func BenchmarkServerStartupLatency(b *testing.B) {
	repoRoot := newLoopgateBenchmarkRepoRoot(b)
	seedBenchmarkAuditLedger(b, repoRoot, 250)
	warmupServer := newLoopgateBenchmarkServer(b, repoRoot)
	warmupServer.CloseDiagnosticLogs()

	b.Run("active_audit_ledger_250_events", func(b *testing.B) {
		b.ReportAllocs()
		durations := make([]int64, b.N)
		socketDir := b.TempDir()

		b.ResetTimer()
		startedAll := time.Now()
		for index := 0; index < b.N; index++ {
			startedAt := time.Now()
			server, err := NewServer(repoRoot, filepath.Join(socketDir, fmt.Sprintf("loopgate-%d.sock", index)))
			durations[index] = time.Since(startedAt).Nanoseconds()
			if err != nil {
				b.Fatalf("new server with seeded audit ledger: %v", err)
			}
			server.CloseDiagnosticLogs()
		}
		elapsed := time.Since(startedAll)
		b.StopTimer()

		reportLatencyPercentiles(b, durations)
		reportThroughput(b, b.N, elapsed)
	})
}

func newLoopgateBenchmarkRepoRoot(b *testing.B) string {
	return newLoopgateBenchmarkRepoRootWithPolicy(b, loopgatePolicyYAML(false))
}

func newLoopgateBenchmarkRepoRootWithPolicy(b *testing.B, policyYAML string) string {
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
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, policyYAML); err != nil {
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

func benchmarkCapabilityToken(controlSessionID string, allowedCapabilities []string) capabilityToken {
	return capabilityToken{
		TokenID:             controlSessionID + "-token",
		Token:               controlSessionID + "-token",
		ControlSessionID:    controlSessionID,
		ActorLabel:          "benchmark-actor",
		ClientSessionLabel:  controlSessionID,
		AllowedCapabilities: capabilitySet(allowedCapabilities),
		PeerIdentity: peerIdentity{
			UID: uint32(os.Getuid()),
			PID: os.Getpid(),
		},
		TenantID:  "benchmark-tenant",
		UserID:    "benchmark-user",
		ExpiresAt: time.Now().UTC().Add(sessionTTL),
	}
}

func benchmarkRequestID(prefix string, index uint64) string {
	return fmt.Sprintf("%s-%d", prefix, index)
}

func seedBenchmarkAuditLedger(b *testing.B, repoRoot string, eventCount int) {
	b.Helper()

	auditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	if err := os.MkdirAll(filepath.Dir(auditPath), 0o700); err != nil {
		b.Fatalf("mkdir benchmark audit ledger dir: %v", err)
	}
	auditRuntime := ledger.NewAppendRuntime()
	for index := 0; index < eventCount; index++ {
		if err := auditRuntime.Append(auditPath, ledger.NewEvent(
			time.Date(2026, 5, 4, 12, 0, index, 0, time.UTC).Format(time.RFC3339Nano),
			"benchmark.seed",
			"benchmark-seed-session",
			map[string]interface{}{"seed_index": index},
		)); err != nil {
			b.Fatalf("seed benchmark audit ledger: %v", err)
		}
	}
}

func writeBenchmarkSandboxFile(b *testing.B, server *Server, sandboxRelativePath string, content string) {
	b.Helper()

	targetPath := filepath.Join(server.sandboxPaths.Home, sandboxRelativePath)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		b.Fatalf("mkdir benchmark sandbox file dir: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte(content), 0o600); err != nil {
		b.Fatalf("write benchmark sandbox file: %v", err)
	}
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

func reportThroughput(b *testing.B, operationCount int, elapsed time.Duration) {
	b.Helper()
	if operationCount <= 0 || elapsed <= 0 {
		return
	}
	b.ReportMetric(float64(operationCount)/elapsed.Seconds(), "ops_per_sec")
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

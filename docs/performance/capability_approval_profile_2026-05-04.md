# Capability And Approval Profile - 2026-05-04

## Summary

This profile investigated the first throughput baseline hotspot:

- `BenchmarkCapabilityExecutionLatency/parallel_fs_read_audited`
- `BenchmarkApprovalCreationLatency/parallel_fs_write_pending`

Both profiles confirm that Loopgate is mostly paying for security-relevant
durability and boundary checks, not pure Go compute.

The immediate optimization target is not "remove audit cost." The safer target
is reducing repeated path-resolution work, reducing audit/UI allocation
pressure, and making overload behavior explicit around these durable paths.

## Commands

Profiles were captured with:

```bash
go test -run=^$ -bench=BenchmarkCapabilityExecutionLatency -benchmem -benchtime=8s -cpuprofile=/tmp/loopgate-profiles/capability.cpu.pprof -memprofile=/tmp/loopgate-profiles/capability.mem.pprof ./internal/loopgate
go test -run=^$ -bench=BenchmarkApprovalCreationLatency -benchmem -benchtime=8s -cpuprofile=/tmp/loopgate-profiles/approval.cpu.pprof -memprofile=/tmp/loopgate-profiles/approval.mem.pprof ./internal/loopgate
```

This local Go toolchain did not include `go tool pprof`, so the standalone
`github.com/google/pprof` tool was installed and used:

```bash
go install github.com/google/pprof@latest
/Users/adalaide/go/bin/pprof -top /tmp/loopgate-profiles/capability.cpu.pprof
/Users/adalaide/go/bin/pprof -top -alloc_space /tmp/loopgate-profiles/capability.mem.pprof
/Users/adalaide/go/bin/pprof -top /tmp/loopgate-profiles/approval.cpu.pprof
/Users/adalaide/go/bin/pprof -top -alloc_space /tmp/loopgate-profiles/approval.mem.pprof
```

## Benchmark Results

Capability execution:

```text
BenchmarkCapabilityExecutionLatency/parallel_fs_read_audited-11
1557 iterations, 6.796895 ms/op, 147.1 ops/sec, p50 74.823 ms, p95 82.943 ms, p99 87.423 ms, 127138 B/op, 1151 allocs/op
```

Approval creation:

```text
BenchmarkApprovalCreationLatency/parallel_fs_write_pending-11
1774 iterations, 6.955328 ms/op, 143.8 ops/sec, p50 75.954 ms, p95 103.049 ms, p99 115.013 ms, 68222 B/op, 617 allocs/op
```

## CPU Findings

### Capability `fs_read`

Top cumulative owners:

- `Server.executeCapabilityRequest`: 79.39%
- `Server.logEvent` / `auditruntime.Runtime.Record`: 56.76%
- `ledger.AppendWithRotation`: 56.42%
- `Server.finalizeCapabilityExecution`: 31.76%
- `FSRead.Execute`: 22.30%
- `tools.resolveValidatedPath`: 16.22%
- `safety.ExplainSafePath`: 15.88%

Top leaf cost:

- `syscall.rawsyscalln`: 69.93%

Interpretation: CPU time is mostly kernel time from filesystem work. The
audited ledger append is the largest required cost. The next distinct runtime
cost is strict path validation and filesystem read resolution.

### Approval Creation

Top cumulative owners:

- `Server.executeCapabilityRequest`: 76.30%
- `Server.logEvent` / `auditruntime.Runtime.Record`: 71.75%
- `ledger.AppendWithRotation`: 70.78%
- `Server.createCapabilityApprovalResponse`: 45.13%
- `ledger.appendPreparedEventToFile`: 20.13%
- `ledger.openLedgerLock`: 14.61%

Top leaf cost:

- `syscall.rawsyscalln`: 61.69%

Interpretation: approval creation is even more audit-bound than capability
read execution. That is expected because creating a pending approval must
persist an authoritative audit event before returning approval state.

## Allocation Findings

### Capability `fs_read`

Top cumulative allocation owners:

- `Server.executeCapabilityRequest`: 96.02%
- `Server.logEvent` / `auditruntime.Runtime.Record`: 48.83%
- `Server.finalizeCapabilityExecution`: 47.96%
- `ledger.AppendWithRotation`: 42.51%
- `FSRead.Execute`: 34.40%
- `tools.resolveValidatedPath`: 33.39%
- `safety.ExplainSafePath`: 32.63%
- `filepath.EvalSymlinks`: 30.60%

Notable flat allocation sites:

- `filepath.walkSymlinks`: 12.65%
- `os.lstatNolog`: 11.89%
- `Server.emitUIEvent`: 9.50%
- `ledger.canonicalizeDataMap`: 7.59%
- `encoding/json.Marshal`: 6.83%

Interpretation: strict path validation is allocation-heavy. Audit JSON
canonicalization and UI event projection are also visible.

### Approval Creation

Top cumulative allocation owners:

- `Server.executeCapabilityRequest`: 93.39%
- `Server.logEvent` / `auditruntime.Runtime.Record`: 58.81%
- `Server.createCapabilityApprovalResponse`: 57.57%
- `ledger.AppendWithRotation`: 46.13%
- `ledger.prepareLedgerEventLine`: 36.11%
- `encoding/json.Marshal`: 22.28%
- `Server.emitUIApprovalPending`: 19.88%
- `ledger.canonicalizeDataMap`: 10.38%

Notable flat allocation sites:

- `Server.emitUIEvent`: 19.50%
- `encoding/json.Marshal`: 9.99%
- `ledger.canonicalizeDataMap`: 10.38%
- `encoding/json.mapEncoder.encode`: 8.07%
- `secrets.RedactText`: 6.63%

Interpretation: approval creation allocation pressure is split between audit
event canonicalization/hashing, approval/UI event projection, and redaction.

## Recommended Next PRs

1. Add profile-backed comments or docs around the audited hot path.

   This helps future agents avoid "optimizing" by weakening audit durability.

2. Inspect strict path-resolution reuse opportunities.

   The `fs_read` path currently pays for strict path validation and symlink
   walking in the capability execution path. Any reuse must preserve the
   no-silent-fallback and no-TOCTOU invariants.

3. Inspect UI event allocation pressure.

   `emitUIEvent` is visible in both capability and approval allocation
   profiles. Look for avoidable copies or overly large event payloads before
   considering any buffering design.

4. Inspect ledger canonicalization allocation.

   `canonicalizeDataMap`, `encoding/json.Marshal`, and `hashEvent` are visible
   in both paths. Changes here are security-sensitive because audit hashes must
   remain deterministic.

5. Add overload-focused tests before changing concurrency behavior.

   Since fsync-bound paths cannot scale infinitely, the production-hardening
   answer should include explicit backpressure and clear denial/error behavior,
   not just micro-optimizations.

## Non-Recommendations

- Do not skip fsync for authoritative audit events.
- Do not cache path validation results across filesystem mutations without a
  specific TOCTOU design.
- Do not remove UI/audit records just to improve benchmark numbers.
- Do not widen concurrency limits until the overload behavior is specified and
  tested.

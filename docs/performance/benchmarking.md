# Loopgate Benchmark Baseline

This document is the agent-facing entrypoint for local performance checks.
Benchmarks are evidence for design decisions; they are not authority to weaken
audit durability, policy checks, or fail-closed behavior.

## Run

```bash
make bench
```

Equivalent explicit command:

```bash
go test -run=^$ -bench=. -benchmem ./internal/loopgate ./internal/ledger
```

Use a fixed benchtime when comparing branches:

```bash
go test -run=^$ -bench=. -benchmem -benchtime=5s ./internal/loopgate ./internal/ledger
```

## Current Coverage

`internal/loopgate/server_benchmark_test.go` covers:

- `/v1/health` handler overhead
- audited hook pre-validation for a read operation
- direct audited server audit append
- in-process `fs_read` capability execution
- `fs_write` approval creation under parallel load
- UI event emission while the replay buffer is full
- server startup with a seeded active audit ledger

`internal/ledger/append_benchmark_test.go` covers:

- serial ledger append with fsync
- parallel ledger append with fsync
- serial ledger append without fsync to isolate JSON/hash/chain overhead

Follow-up profile:

- `docs/performance/capability_approval_profile_2026-05-04.md` records the
  first CPU/allocation profile for capability execution and approval creation.

## Reading The Numbers

Go always reports `ns/op`. These benchmarks also report:

- `p50_us`: median latency; half of operations are faster than this
- `p95_us`: tail latency for the slowest 5 percent
- `p99_us`: tail latency for the slowest 1 percent
- `ops_per_sec`: observed throughput for that benchmark run
- `B/op` and `allocs/op`: memory allocation pressure per operation

For Loopgate, p95 and p99 matter more than p50 because overload risk usually
shows up in the tail first.

## Expected Hotspots

Audit append with fsync is expected to dominate audited paths. That is mostly a
healthy cost: the audit ledger is the durable authority record. Optimize around
bounded concurrency, batching for export, startup anchors, and backpressure
before considering any durability tradeoff.

Loopgate has two configurable overload guardrails in `config/runtime.yaml`:

- `control_plane.max_in_flight_http_requests` bounds total concurrent local
  control-plane HTTP handlers.
- `control_plane.max_in_flight_capability_executions` bounds the narrower
  capability execution and approval-creation authority path so health, status,
  and config routes can remain available while tool/audit work is saturated.

When the capability execution limit is saturated, Loopgate fails closed with
`control_plane_state_saturated` and metadata that marks the denial as
retryable for agents.

## Initial Local Baseline

Captured on 2026-05-04 with:

```bash
go test -run=^$ -bench=. -benchmem -benchtime=1s ./internal/loopgate ./internal/ledger
```

Machine-reported test CPU: `Apple M3 Pro`.

| Benchmark | ns/op | p50_us | p95_us | p99_us | ops_per_sec |
| --- | ---: | ---: | ---: | ---: | ---: |
| health route, parallel | 1,062 | 1.667 | 26.83 | 154.6 | n/a |
| hook pre-validate read, audited parallel | 3,414,931 | 37,648 | 43,895 | 47,880 | n/a |
| server audit append, parallel fsync | 3,116,262 | 34,244 | 39,965 | 43,889 | n/a |
| capability `fs_read`, audited parallel | 6,330,663 | 67,105 | 80,962 | 82,952 | 158.0 |
| approval creation `fs_write`, parallel | 6,324,650 | 70,504 | 86,786 | 91,081 | 158.1 |
| server startup with 250 active audit events | 1,097,537 | 1,043 | 1,297 | 1,406 | 911.1 |
| ledger append, serial fsync | 2,748,285 | 2,974 | 3,528 | 3,972 | 363.9 |
| ledger append, parallel fsync | 2,652,122 | 31,902 | 33,950 | 35,085 | 377.1 |
| ledger append, serial no fsync | 33,207 | 30.58 | 43.75 | 63.96 | 30,114 |

Interpretation:

- Raw ledger append without fsync is much faster, which confirms fsync is the
  dominant durable-audit cost.
- Audited hook validation tracks close to direct server audit append, so audit
  durability is the main floor for that path.
- Capability execution and approval creation add meaningful overhead on top of
  audit append. That overhead is the next place to inspect before adding more
  enterprise control-plane behavior.
- Startup over a 250-event active ledger is currently not the scary path in
  this small baseline; keep measuring as ledger size and segmented history grow.

## Change Rules

- Keep benchmarks daemon-free and external-service-free.
- Do not make benchmark-only production shortcuts.
- Do not disable audit, policy, path validation, request replay, or secret
  redaction to improve benchmark output.
- When changing a hot path, include before/after benchmark output in the PR.
- If a benchmark is intentionally synthetic, name what it excludes.

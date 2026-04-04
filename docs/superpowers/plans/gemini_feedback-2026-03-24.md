**Last updated:** 2026-03-24

# Gemini Security Code Review & Architecture Findings

**Date:** 2026-03-24
**Reviewer:** Gemini (Independent Deep-Dive Analysis)
**Target:** Loopgate core packages (`internal/loopgate`, `internal/sandbox`, `internal/secrets`)

## Overview

This report supplements the previous findings by Claude and the ChatGPT TCL analysis. While those reviews correctly identified high-level structural gaps in memory and audit ordering, this review utilized a code-review subagent to deeply analyze concurrency (TOCTOU), locking granularity, and filesystem escape edge-cases that are notoriously difficult to spot. 

Overall, the architecture's IAM-grade approach is highly robust. However, Go's lack of native transactional state rollbacks introduces several partial-state vulnerabilities during complex mutations.

---

## 1. Critical Security Vulnerabilities

### FINDING 1: Symlink Sandbox Escape via Lexical Path Verification (TOCTOU)
*   **Confidence:** 95
*   **Severity:** CRITICAL
*   **Location:** `internal/sandbox/sandbox.go` and `internal/loopgate/morphling_workers.go` (stageMorphlingCompletionArtifacts)
*   **Description:** `ResolveHomePath` returns a purely lexical path. A compromised morphling can create a symlink (`ln -s ../other_morphling escape`). When staging `escape/secret.txt`, the lexical path appears valid, but reading the artifact traverses the symlink into a sibling morphling's workspace.
*   **Fix:** `ResolveHomePath` must return the true resolved relative path *after* `filepath.EvalSymlinks`, enforcing that the evaluated path strictly resides within the evaluated absolute path of the morphling's directory.

---

## 2. Concurrency & State Invariant Violations

### FINDING 2: Ledger/State Divergence on Memory Persistence Failure
*   **Confidence:** 90
*   **Severity:** HIGH
*   **Location:** `internal/loopgate/continuity_memory.go` (`mutateContinuityMemory`)
*   **Description:** Mutation events are appended to the ledger, and an audit is logged. Then, `saveMemoryState` is called. If disk serialization fails, the function errors out and *does not update the in-memory `server.memoryState`*. The ledger and running state are permanently diverged until reboot.
*   **Fix:** In-memory state MUST be updated immediately after the ledger append succeeds. Snapshot failures should be logged as warnings but not halt the state update.

### FINDING 3: Approval State Transition Without Guaranteed Audit Persistence
*   **Confidence:** 85
*   **Severity:** HIGH
*   **Location:** `internal/loopgate/server_capability_handlers.go` (`validateAndRecordApprovalDecision`)
*   **Description:** Approvals are atomically flipped to `approvalStateConsumed` in memory. If the subsequent `server.logEvent` fails, the capability is correctly denied, but the approval is permanently consumed with zero audit record of the operator's decision.
*   **Fix:** Audit log failures must roll back the approval state transition so the operator can retry, preserving the invariant that security actions must be observable.

### FINDING 4: TOCTOU Race Condition in `openMorphlingWorkerSession`
*   **Confidence:** 85
*   **Severity:** HIGH
*   **Location:** `internal/loopgate/morphling_workers.go` (`openMorphlingWorkerSession`)
*   **Description:** The function drops `server.mu.Lock()` to fetch records, then re-acquires it to issue the session. Two concurrent requests with the same launch token will both pass the initial check. The second will silently call `revokeMorphlingWorkerAccessLocked`, deleting the session the first thread just successfully received.
*   **Fix:** Hold `server.mu.Lock()` continuously for the launch token validation, consumption, and session creation.

### FINDING 5: Orphaned Sandbox Artifacts on Concurrent Termination
*   **Confidence:** 80
*   **Severity:** MEDIUM
*   **Location:** `internal/loopgate/morphling_workers.go` (`completeMorphlingWorker`)
*   **Description:** Artifacts are copied to the `Outputs` directory outside the lock. If a concurrent `terminateMorphling` call transitions the state to `Terminating`, the final state transition fails, but the partially staged artifacts are left orphaned in the directory.
*   **Fix:** Add a `defer` cleanup handler to delete the staged artifacts if the final state transition fails.

---

## 3. Architecture & Observability Gaps

### FINDING 6: Brittle Regex-Based Secret Redaction Bypass
*   **Confidence:** 80
*   **Severity:** MEDIUM
*   **Location:** `internal/secrets/redact.go` (`RedactText`)
*   **Description:** Redaction relies on strict regular expressions (e.g., `password="..."`). If a model dumps poorly formatted JSON or shell output where a token spans lines or spaces, the regex breaks, leaking the secret into the plaintext append-only ledger. Furthermore, if a user pastes an unknown key into the chat, the system will not recognize it.
*   **Fix:** Implement a Hybrid Defense-in-Depth model: 
    1. **Primary Layer (Exact Match):** Inject exact known secret values (from `server.providerTokens` or Keychain) into an Aho-Corasick trie to scrub exact matches from all unstructured outputs before ledger persistence. 
    2. **Secondary Layer (Entropy Scanner):** Run a scanner over the text looking for high-entropy strings. If a high-entropy string is detected leaving the sandbox, throw a Loopgate Policy Alert asking the user if it should be quarantined or added to the vault.

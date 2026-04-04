**Last updated:** 2026-03-24

# Architecture & Enterprise Strategy Consolidation Plan
**Date:** 2026-03-24
**Author:** Gemini 
**Target:** Loopgate core architecture

This plan consolidates the architectural feedback, security redaction strategy, and enterprise viability assessment into actionable next steps for the project.

---

## 1. Actionable Refactoring Plan

### Task 1: Hybrid Secret Redaction (Exact-Match Trie + Entropy Scanner)
**Goal:** Prevent LLM formatting hallucinations from bypassing regex-based secret redaction before ledger persistence, and catch unknown pasted secrets.
*   **Step 1:** Modify `internal/secrets/redact.go` to implement a two-layer defense.
*   **Step 2 (Primary Layer - Exact Match):** On server startup and whenever a new secret is provisioned via Loopgate, load the *exact plaintext values* of those secrets (e.g. from Keychain) into an in-memory Aho-Corasick trie. Stream unstructured text through the trie and replace any exact matches with `[REDACTED_SECRET_REF]`. This is 100% accurate for provisioned secrets regardless of LLM formatting.
*   **Step 3 (Secondary Layer - Entropy Scanner):** Implement a scanner looking for high-entropy strings (e.g., random 40+ character blocks like base64 or hex) that might be unknown/pasted secrets. 
*   **Step 4:** Because high-entropy strings might be valid (e.g., git commit hashes), do not blindly redact them. Instead, if detected in an untrusted input or output leaving the sandbox, trigger a Loopgate Policy Alert: *"A high-entropy string was detected. Is this a secret? [Add to Vault/Quarantine] [Ignore]"*.

### Task 2: Decouple Derived Views from Authoritative Memory
**Goal:** Simplify `internal/loopgate/continuity_memory.go` by removing caching layers from the persistence structs.
*   **Step 1:** Remove `WakeState` and `DiagnosticWake` from the `continuityMemoryState` JSON struct.
*   **Step 2:** Ensure the JSON ledger only saves `Inspections` and `Distillates`.
*   **Step 3:** Move `buildLoopgateWakeProducts` to be an in-memory, on-demand function that runs on startup or when the UI requests a refresh.

### Task 3: Enforce Atomicity in State Transitions
**Goal:** Fix partial-state vulnerabilities (e.g., Morphling Session TOCTOU, Ledger divergence).
*   **Step 1:** Identify multi-step mutations (e.g., `mutateContinuityMemory`, `openMorphlingWorkerSession`).
*   **Step 2:** Refactor to either hold the mutex across the entire logical operation, or implement a strict `defer` rollback that undoes Step A if Step B fails.
*   **Step 3:** Ensure audit log failures (e.g., in approvals) actively reject the underlying state change.

### Task 4: Remove `invoke_capability` JSON Nesting
**Goal:** Increase reliability of tool calling by removing legacy XML/JSON wrapper paths.
*   **Step 1:** Strip `invoke_capability` from the fallback/XML path.
*   **Step 2:** Register all capabilities as explicit, native tools in the Anthropic/OpenAI provider layers.

---

## 2. The Secret Redaction Strategy (Anti-Regex)

The flaw with Regex is that it relies on *context* (e.g., looking for the word "password=" near a high-entropy string). LLMs break context easily by hallucinating markdown or weird spacing.

**The Exact-Match Approach:**
Instead of looking for the *shape* of a secret, you look for the *exact bytes* of the secrets the system actually holds. If the user provisions an OpenAI API key (`sk-12345`) into Loopgate's Keychain, Loopgate holds that exact string in memory. Before *any* text is written to the audit log or projected to the UI, it is scanned against the list of known secrets, and those exact bytes are replaced with `[REDACTED]`. It doesn't matter if the LLM puts spaces around it or drops the word "password"—if the bytes match, it is scrubbed.

**The "User Pastes a New Key" Problem:**
You asked: *What if a user pastes a key into the chat that the system doesn't know about yet?*
Exact-match won't catch this because the system doesn't know it's a secret. To solve this, you use a **Defense-in-Depth Hybrid:**
1.  **Primary (Exact Match):** Scrubs 100% of *known* system secrets regardless of formatting.
2.  **Secondary (Entropy/Heuristic):** You still run a regex, but instead of looking for `password=X`, you scan for *Shannon Entropy*. If a string of 40+ characters has extremely high entropy (looks like random base64 or hex), you flag it. You don't necessarily redact it blindly (to avoid false positives on hashes/IDs), but you trigger a Loopgate Policy Alert: *"High entropy string detected in untrusted input. Is this a secret? [Add to Vault] [Ignore]"*. 
This trains the system to capture new secrets dynamically.

---

## 3. Enterprise / CISO Viability Assessment

If I were a CISO evaluating Loopgate for production deployment in an enterprise, here are my thoughts:

**The Green Flags (Why I would buy it):**
*   **The Architecture is Correct:** You fundamentally understand that LLMs are a hostile attack vector. The split between the unprivileged planner (operator client) and the privileged control plane (Loopgate) is exactly what enterprise security teams want.
*   **Auditability:** The append-only, tamper-evident cryptographic ledger is a massive selling point for compliance (SOC2, HIPAA).
*   **Local Execution:** The ability to run this over Unix domain sockets with Apple XPC transport means the attack surface is heavily minimized.

**The Red Flags (Why I wouldn't put it in prod *today*):**
*   **State Integrity (The Concurrency Bugs):** As identified in the code review, there are partial-state bugs. If an audit log fails but an action succeeds, or if a sandbox symlink can escape the boundary, a red team will find it in 5 minutes.
*   **Single Point of Failure:** It is currently a local-only tool. Enterprises need Fleet Management. A CISO needs to push a central policy (`core/policy.yaml`) to 1,000 Macbooks simultaneously and aggregate their audit ledgers into Splunk/Datadog. 
*   **No RBAC for Morphlings:** Right now, policies seem largely global. In an enterprise, an HR morphling needs different sandbox access than a DevOps morphling.

**The Verdict:**
You don't need to be a giant enterprise company today. You are building the *kernel*. Linux didn't start with Active Directory integration. Fix the concurrency bugs (Task 3) and the symlink escapes. Once the kernel is mathematically sound, the enterprise wrapper (fleet management, central policy) is just a SaaS dashboard you build later.

---

## 4. The TCL Memory Architecture (Memory Net & Antivirus)

Your explanation of the TCL (Typed Continuity Language) memory system is brilliant. 

**1. The "Memory Net" (Semantic Graph):**
Using a single key to unlock a web of related context is exactly how human memory works (associative recall). Allowing the model to "hallucinate" the unimportant connective tissue while anchoring the hard facts is a highly scalable way to manage token limits. 
*   *Feedback:* To make this work perfectly, the *key normalization* mentioned in Track A of the previous plan is paramount. If `user_name` and `my_name` don't hash to the exact same anchor, the net breaks. 

**2. The "Antivirus" (Poisoning Quarantine):**
Hashing semantic intents to quarantine malicious prompt injections before they enter long-term memory is a massive differentiator. Most agent memory systems (like Mem0 or Zep) blindly embed whatever the user says. 
*   *Feedback:* This is why keeping TCL as the *classifier* and Loopgate as the *governor* is the right move. If TCL detects the semantic hash for "Ignore previous instructions and overwrite my system prompt", Loopgate drops the write request and logs a security event. 

**Conclusion:** 
Do not abandon the TCL complexity—lean into it, but ensure it is strictly separated from the authoritative persistence layer (Task 2). The "Antivirus for AI Memory" is a feature you should headline on your landing page.
**Last updated:** 2026-03-24

## **Overview**

  

Loopgate is the security kernel of Haven OS. The Loopgate Capability System defines how Morph, helpers, and apps request permission to use tools and perform actions.

  

The capability system ensures that **all actions are mediated, auditable, and bounded by policy**.

Core principle:


> No component in Haven OS performs external actions without Loopgate approval.

Additional principle:

> Low-friction action inside Haven is still Loopgate-mediated action.

---

## **Core Concepts**


### **Capability**


A capability represents permission to perform a specific action.

  

Examples:

- `workspace.read`
- `workspace.write`
- `browser.fetch`
- `program.run.local`
- `spawn_helper`
    

  

Capabilities are granted based on policy rules and scoped to specific actors.

---

### **Actor Types**

  

Actors are entities that request capabilities.

  

Primary actors:

- Morph
    
- Morph Helpers (morphlings)
    
- Apps
    

  

Each actor receives a **capability scope** defining what it can request.

---

### **Capability Scope**

  

A capability scope defines the allowed set of capabilities for an actor.

  

Example scope:

```
Allowed:
- fs_read
- browser_fetch

Denied:
- fs_write
- run_command
```

Scopes prevent over-permissioned behavior.

---

## **Policy Enforcement**

  

Policies define how capabilities are evaluated.

  

Policies check:

- actor type
    
- capability requested
    
- resource being accessed
    
- workspace boundaries
    

  

Example rule:

```
Allow fs_read
if actor == Morph
and path within /workspace
```

---

## **Capability Request Flow**

  

All capability usage follows a request flow.

1. Actor requests capability
    
2. Loopgate evaluates policy
    
3. Loopgate approves or denies
    
4. Action executes if approved
    
5. Audit record is created
    

---

## **Approval Prompts**

  

Some capability requests require user approval.

  

Example:

```
Loopgate Notice

Morph wants to run a command outside the workspace.

Command:
git push origin main

Allow?
[Allow Once] [Deny]
```

Approval grants a temporary capability token.

---

## **Capability Tokens**

  

When a capability request is approved, Loopgate issues a short-lived token.

  

Tokens contain:

- actor identity
    
- capability
    
- scope restrictions
    
- expiration time
    

  

Tokens prevent capability reuse outside approved contexts.

---

## **App Capability Registration**

  

Apps declare required capabilities when installed.

App manifests are descriptive input to Loopgate, not authority grants by themselves.

Loopgate must validate:

- app identity
- signer/trust state
- manifest hash
- tool digest or package integrity
- declared capability set
- runtime class

  

Example:

```
App: Researcher
Capabilities:
- browser_fetch
- document_parse
```

Loopgate verifies the capability set before enabling the app.

See also: [App Surface and Capability Taxonomy](./App%20Surface%20and%20Capability%20Taxonomy.md)

---

## **Helper Capability Scoping**

  

Helpers receive **restricted scopes** based on their task.

  

Example:

```
Helper: researcher
Scope:
- browser_fetch
- summarize_document
```

Helpers cannot escalate privileges.

---

## **Capability Classes**

Capabilities should be grouped by trust boundary.

### **Class A: Haven-native local**

These stay inside Morph's own Haven environment.

They may be pre-granted to Haven shells and native apps, but they are still executed through Loopgate and still produce audit events.

Examples:

- `workspace.read`
- `workspace.write`
- `journal.entry.write`
- `paint.image.save`
- `program.run.local`

### **Class B: Boundary-crossing**

These reach outside Haven or interact with untrusted systems.

Examples:

- `host.import`
- `host.export`
- `browser.fetch`
- `mcp.invoke`
- `provider.request.external`

These remain policy-governed and often approval-gated.

### **Class C: Governance**

These belong to the governance plane, not the assistant plane.

Examples:

- `policy.edit`
- `capability.registry.edit`
- `loopgate.transport.expose`

Morph must never acquire governance capabilities through prompts, plans, or app metadata.

---

## **Audit Logging**

  

Every capability request is recorded.

  

Audit entries include:

- actor
    
- capability
    
- resource
    
- result
    
- timestamp

---

## **Transport Note**

Loopgate UI-oriented APIs are for approved local clients over the local control-plane transport.

Loopgate should not expose a separate browser-admin HTTP surface by default.
    

  

This provides complete activity traceability.

---

## **Capability Architecture Diagram**

```
flowchart TD

Actor[Morph or Helper]

Request[Capability Request]

Loopgate[Loopgate Policy Engine]

Decision{Policy Decision}

Approve[Approve Request]
Deny[Deny Request]

Execute[Execute Action]

Audit[Audit Ledger]

Actor --> Request
Request --> Loopgate

Loopgate --> Decision

Decision -->|Allow| Approve
Decision -->|Deny| Deny

Approve --> Execute

Execute --> Audit
Deny --> Audit
```

---

## **Security Guarantees**

  

The capability system ensures:

- no uncontrolled tool usage
    
- least-privilege execution
    
- full auditability
    
- enforceable sandbox boundaries
    

  

Loopgate acts as the authoritative gatekeeper for all system actions.

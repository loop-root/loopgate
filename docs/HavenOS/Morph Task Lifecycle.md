**Last updated:** 2026-03-24

# **Morph Task Lifecycle (Haven OS)**

  

## **Overview**

  

The Morph Task Lifecycle defines the standard flow every request follows inside Haven OS. It ensures predictable behavior, security mediation through Loopgate, and full user visibility.

  

High level flow:

  

User request → Morph planning → Loopgate validation → execution → artifact creation → review → completion.

  

Design rule:

  

**Morph never executes actions outside Loopgate mediation.**

---

## **Phase 1 — Request Intake**

  

Requests originate from the **Morph Messenger** interface.

  

Example:

  

User: “Analyze this repository and suggest improvements.”

  

A task object is created.

  

Example metadata:

```
Task ID: 0841
Source: Messenger
Goal: Analyze repository
Context: /workspace/imports/repo
Priority: normal
```

Morph acknowledges the request and begins planning.

---

## **Phase 2 — Planning**

  

Morph constructs a **Task Plan** describing the steps required.

  

Example:

```
Plan
1. Scan repository structure
2. Read core modules
3. Research best practices
4. Generate improvement suggestions
```

Morph determines:

- required tools
    
- whether helpers are needed
    
- required permissions
    

  

The plan is submitted to Loopgate for validation.

---

## **Phase 3 — Security Validation**

  

Loopgate verifies:

- capability permissions
    
- workspace boundaries
    
- helper limits
    
- policy compliance
    

  

Possible results:

  

Approved → continue execution

  

Requires approval → prompt user

  

Denied → Morph revises the plan

  

Example prompt:

```
Loopgate Notice

Morph wants internet access for research.

Allow?
[Allow Once] [Deny]
```

---

## **Phase 4 — Execution Mode**

  

Morph chooses execution strategy.

  

### **Mode A — Direct Execution**

  

Morph performs the task itself.

  

Examples:

- reading files
    
- summarizing documents
    
- generating code
    
- answering questions
    

  

Even here, tool access requires Loopgate mediation.

  

Example:

```
Capability Request
fs_read: /workspace/projects/main.go
```

---

### **Mode B — Helper Delegation**

  

Morph spawns helper workers for larger tasks.

  

Examples:

- internet research
    
- large code analysis
    
- parallel document processing
    

  

Example spawn:

```
Spawn Helper
Type: researcher
Scope: internet_access
Task: find documentation for library
```

Constraints:

- maximum helpers (default: 5)
    
- limited permissions
    
- time-limited lifespan
    

  

All helpers operate through Loopgate.

---

## **Phase 5 — Artifact Generation**

  

Results are stored as artifacts.

  

Artifacts include:

- summaries
    
- patches
    
- research notes
    
- generated files
    

  

Location:

```
/workspace/artifacts
```

Example:

```
artifacts/
 ├─ repo-analysis.md
 ├─ suggested-refactor.patch
 └─ dependency-notes.txt
```

External systems are not modified automatically.

---

## **Phase 6 — Review & Approval**

  

Before leaving Haven workspace, results require user review.

  

Example prompt:

```
Morph has prepared changes.

Artifacts
repo-analysis.md
refactor.patch

Apply changes to repository?

[Apply]
[Modify]
[Discard]
```

Loopgate enforces sandbox boundaries.

---

## **Phase 7 — Completion**

  

The task is finalized and recorded.

  

Metadata stored:

```
Task ID
Plan
Capabilities used
Artifacts produced
Execution time
Audit trail
```

Tasks remain accessible in Messenger under **Tasks**.

---

## **Behavioral Rules**

  

Morph always plans before acting.

  

Helpers are limited (default maximum: 5).

  

Workspace operations are preferred.

  

External actions require explicit approval.

  

All tool usage is mediated by Loopgate.

---

## **Activity Visibility**

  

Users can monitor progress through the Activity Monitor.

  

Example:

```
Task: Repository Analysis

Status: running

Steps
✓ scanning repo
✓ reading modules
• researching documentation
• generating suggestions
```

Helper workers also appear in the monitor.

---

## **Lifecycle Diagram**

```
flowchart TD

User[User Request]

User --> Messenger[Morph Messenger]

Messenger --> Morph[Morph Assistant]

Morph --> Plan[Task Planning]

Plan --> Validate[Loopgate Validation]

Validate -->|Approved| Mode{Execution Mode}

Validate -->|User Approval Required| Prompt[User Prompt]

Prompt --> Validate

Mode -->|Direct| Direct[Direct Execution]

Mode -->|Helpers| Spawn[Spawn Helpers]

Spawn --> Helpers[Helper Workers]

Direct --> Artifacts[Generate Artifacts]

Helpers --> Artifacts

Artifacts --> Review[User Review]

Review -->|Approved| Complete[Task Complete]

Review -->|Modify| Morph

Review -->|Discard| Complete
```

---

## **Why This Lifecycle Matters**

  

This lifecycle ensures:

- predictable behavior
    
- security enforcement
    
- clear user visibility
    
- reversible outcomes
    

  

Morph remains powerful while staying safe and transparent.
**Last updated:** 2026-03-24

# **Haven Workspace Model**

  

## **Overview**

  

The Haven Workspace is the sandboxed environment where Morph and its helpers operate. It acts as the primary filesystem and execution context for AI activity within Haven OS. The workspace ensures safety by isolating operations from the user’s real system until explicit approval is granted.

---

## **Workspace Goals**

  

The workspace is designed to:

- Provide a safe environment for AI operations
    
- Prevent accidental modification of host files
    
- Stage artifacts for user review
    
- Maintain clear boundaries between internal and external systems
    

---

## **Default Directory Structure**

Haven presents a user-friendly workspace view. The underlying sandbox uses different internal directory names; Haven maps them automatically.

```
/workspace (Haven view)         Sandbox internal path
 ├── projects                   workspace/
 ├── research                   scratch/
 ├── imports                    imports/
 ├── artifacts                  outputs/
 ├── memory                     (separate memory subsystem)
 └── apps                       (future)
```

### **projects**

Active projects Morph is working on. Sandbox internal path: `workspace/`.

### **research**

Temporary research material, scraped pages, summaries. Sandbox internal path: `scratch/`.

### **imports**

User-imported files and folders copied into the workspace. Sandbox internal path: `imports/`.

### **artifacts**

Generated outputs waiting for review. Sandbox internal path: `outputs/`.

### **memory**

Assistant memory and long-term context storage. Managed by the separate Loopgate memory subsystem, not stored as workspace files.

### **apps**

Installed Haven apps and tool bundles. (Future phase.)

---

## **Import Model**

Users import files via file selection dialogs or programmatic path import. Drag-and-drop is planned via Wails `OnFileDrop`.

### Import methods (implemented)

| Method | Trigger | Go binding |
|--------|---------|------------|
| File picker | Attach button (+) in chat input bar, or "Import File" in workspace panel | `WorkspaceImportFile()` |
| Directory picker | "Import Folder" in workspace panel | `WorkspaceImportDirectory()` |
| Programmatic | Chat attach / future drag-and-drop | `WorkspaceImportPath(hostPath)` |

### Import flow

1. User clicks attach (+) or "Import File" in workspace panel
2. Native OS file dialog opens
3. Haven copies the selected file atomically into `imports/` via Loopgate `POST /v1/sandbox/import`
4. Workspace panel refreshes to show the imported file
5. Morph operates on the copy

Original files remain unchanged until export approval.

---

## **Export Model**

When Morph produces changes:

1. Morph stages outputs via Loopgate `POST /v1/sandbox/stage` — artifacts appear in `artifacts/`
2. User opens workspace panel, navigates to `artifacts/`
3. Each artifact file shows an **Export** button
4. Clicking Export opens a native save dialog via `WorkspaceExport(sandboxPath)`
5. Loopgate copies the staged file to the chosen host destination via `POST /v1/sandbox/export`

No external files are modified automatically. Every export requires explicit user action through the save dialog.

---

## **Memory Storage**

Morph memory is managed by the Loopgate memory subsystem (`/v1/memory/*` endpoints), not as files in the workspace directory. This includes:

- task summaries
- user preferences
- contextual notes

Memory allows Morph to remain persistent across sessions. The `memory` directory appears in the Haven workspace view for conceptual completeness but is not yet implemented as a workspace-resident folder.

---

## **App Installation**

  

Apps install into /workspace/apps.

  

Apps are bundles containing:

- tool definitions
    
- capability scopes
    
- helper roles
    

  

Loopgate enforces permission boundaries for all app capabilities.

---

## **Browser Sandbox**

  

The research browser operates inside the workspace.

  

All downloaded content is stored in /workspace/research.

  

Network access is mediated by Loopgate.

---

## **Workspace Isolation**

Isolation rules:

- No direct access to host filesystem — all paths resolved via `sandbox.ResolveHomePath`, which rejects traversal outside `/morph/home`
- Symlinks are rejected — `CopyPathAtomic` skips symlinks, `ReadDir` in list handler skips them
- No direct network access without approval
- All commands mediated by Loopgate capability tokens and request signatures

---

## **Loopgate Sandbox API**

The workspace is accessed exclusively through Loopgate endpoints:

| Endpoint | Purpose |
|----------|---------|
| `POST /v1/sandbox/import` | Copy host file/directory into sandbox `imports/` |
| `POST /v1/sandbox/stage` | Stage a sandbox file as a reviewable artifact in `outputs/` |
| `POST /v1/sandbox/metadata` | Get metadata (SHA256, size, stage time) for a staged artifact |
| `POST /v1/sandbox/export` | Copy a staged artifact back to the host (requires explicit path) |
| `POST /v1/sandbox/list` | List directory contents within the sandbox home |

All operations require authentication, request signing, and produce audit log entries.

---

## **Haven Workspace UI**

The Messenger includes a toggleable workspace panel (right side):

- **Breadcrumb navigation** — click path segments to navigate (`~ / projects / myapp`)
- **Directory browser** — folders are clickable, files show size
- **Import toolbar** — "Import File" and "Import Folder" buttons open native OS dialogs
- **Export buttons** — visible on files in the `artifacts/` view
- **Attach button** — `+` in chat input bar for quick file import during conversation
- **Name mapping** — sandbox directories appear with Haven-friendly names (projects, artifacts, research)

---

## **Workspace Architecture Diagram**

```mermaid
flowchart TD
    User[User]

    subgraph Messenger["Haven Messenger"]
        AttachBtn[Attach Button]
        WsPanel[Workspace Panel]
    end

    subgraph Loopgate["Loopgate Sandbox API"]
        Import[/v1/sandbox/import]
        List[/v1/sandbox/list]
        Stage[/v1/sandbox/stage]
        Export[/v1/sandbox/export]
    end

    subgraph Sandbox["Sandbox Filesystem"]
        Imports[imports/]
        Workspace[workspace/]
        Scratch[scratch/]
        Outputs[outputs/]
    end

    User -->|file picker| AttachBtn
    User -->|browse| WsPanel
    AttachBtn --> Import
    WsPanel --> List
    WsPanel -->|export button| Export

    Import --> Imports
    List --> Sandbox
    Stage --> Outputs
    Export -->|save dialog| User
```

---

## **Summary**

The Haven Workspace provides the physical environment where Morph performs work. It ensures safe operations through isolation, staged outputs, and clear boundaries between internal activity and external systems. The workspace is fully implemented with Loopgate sandbox endpoints, Haven Wails bindings, and a Messenger workspace panel for browsing, importing, and exporting files.
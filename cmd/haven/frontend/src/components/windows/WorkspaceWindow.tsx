import {
  WorkspaceCreateDir,
  WorkspaceImportFile,
  WorkspacePreviewFile,
  WorkspaceRename,
  WorkspaceWriteFile,
} from "../../../wailsjs/go/main/HavenApp";
import type { WorkspaceListEntry } from "../../../wailsjs/go/main/HavenApp";
import type { PreviewState } from "../../lib/haven";
import { WORKSPACE_ICONS, formatSize, formatTime, isPreviewable } from "../../lib/haven";

interface WorkspaceEditState {
  path: string;
  content: string;
  dirty: boolean;
}

interface WorkspaceNewNameState {
  type: "file" | "dir";
}

export interface WorkspaceWindowProps {
  wsPath: string;
  wsEntries: WorkspaceListEntry[];
  wsLoading: boolean;
  wsError: string;
  preview: PreviewState | null;
  wsEditing: WorkspaceEditState | null;
  wsRenaming: string | null;
  wsRenameValue: string;
  wsNewName: WorkspaceNewNameState | null;
  wsNewNameValue: string;
  onLoadWorkspace: (path: string) => void;
  onSetWsError: (error: string) => void;
  onClearWsError: () => void;
  onSetPreview: (preview: PreviewState | null | ((prev: PreviewState | null) => PreviewState | null)) => void;
  onSetWsEditing: (value: WorkspaceEditState | null | ((prev: WorkspaceEditState | null) => WorkspaceEditState | null)) => void;
  onSetWsRenaming: (path: string | null) => void;
  onSetWsRenameValue: (value: string) => void;
  onSetWsNewName: (value: WorkspaceNewNameState | null) => void;
  onSetWsNewNameValue: (value: string) => void;
  onRequestCloseEditor: () => void;
  onRequestDeleteEntry: (path: string, entryName: string) => void;
  onRequestReviewPath: (path: string) => void;
  onExportPath: (path: string) => void;
}

export default function WorkspaceWindow({
  wsPath,
  wsEntries,
  wsLoading,
  wsError,
  preview,
  wsEditing,
  wsRenaming,
  wsRenameValue,
  wsNewName,
  wsNewNameValue,
  onLoadWorkspace,
  onSetWsError,
  onClearWsError,
  onSetPreview,
  onSetWsEditing,
  onSetWsRenaming,
  onSetWsRenameValue,
  onSetWsNewName,
  onSetWsNewNameValue,
  onRequestCloseEditor,
  onRequestDeleteEntry,
  onRequestReviewPath,
  onExportPath,
}: WorkspaceWindowProps) {
  const locationLabel = wsPath || "home";
  const workspaceMode = wsEditing ? "editing" : preview ? "previewing" : wsLoading ? "loading" : "browsing";

  return (
    <div className="workspace-app">
      <div className="app-hero workspace-hero">
        <div className="app-kicker">Workspace</div>
        <div className="app-title">Where Morph reads, edits, and stages work inside Haven.</div>
        <div className="app-subtitle">
          Everything here stays inside Haven's mediated workspace until you intentionally export it back out.
        </div>
        <div className="app-summary-pills">
          <span className="app-summary-pill">location {locationLabel}</span>
          <span className="app-summary-pill">{wsEntries.length} item{wsEntries.length === 1 ? "" : "s"}</span>
          <span className="app-summary-pill">{workspaceMode}</span>
        </div>
      </div>

      <div className="ws-toolbar">
        <div className="ws-toolbar-left">
          <button
            className="ws-tb-btn"
            title="Back"
            onClick={() => {
              if (!wsPath) return;
              const parts = wsPath.split("/").filter(Boolean);
              parts.pop();
              onLoadWorkspace(parts.join("/"));
            }}
            disabled={!wsPath}
          >
            {"\u25C0"}
          </button>
          <div className="ws-breadcrumb">
            <span className="ws-crumb-link" onClick={() => onLoadWorkspace("")}>~</span>
            {wsPath && wsPath.split("/").map((part, index, parts) => {
              const currentPath = parts.slice(0, index + 1).join("/");
              return (
                <span key={currentPath}>
                  <span className="ws-crumb-sep">/</span>
                  <span className="ws-crumb-link" onClick={() => onLoadWorkspace(currentPath)}>{part}</span>
                </span>
              );
            })}
          </div>
        </div>
        <div className="ws-toolbar-right">
          <button className="ws-tb-btn" title="New File" onClick={() => { onSetWsNewName({ type: "file" }); onSetWsNewNameValue(""); }}>+{"\u{1F4C4}"}</button>
          <button className="ws-tb-btn" title="New Folder" onClick={() => { onSetWsNewName({ type: "dir" }); onSetWsNewNameValue(""); }}>+{"\u{1F4C1}"}</button>
          <button className="ws-tb-btn" title="Import File" onClick={() => WorkspaceImportFile().then(() => onLoadWorkspace(wsPath))}>{"\u{1F4E5}"}</button>
        </div>
      </div>

      {wsError && <div className="ws-error">{wsError}<button className="ws-error-close" onClick={onClearWsError}>&times;</button></div>}

      <div className="ws-finder-body">
        <div className="ws-sidebar">
          <div className="ws-sidebar-title">Locations</div>
          <div className={`ws-sidebar-item ${wsPath === "" ? "active" : ""}`} onClick={() => onLoadWorkspace("")}>
            <span className="ws-sidebar-icon">{"\u{1F3E0}"}</span> Home
          </div>
          {Object.entries(WORKSPACE_ICONS).map(([name, icon]) => (
            <div key={name} className={`ws-sidebar-item ${wsPath === name || wsPath.startsWith(`${name}/`) ? "active" : ""}`} onClick={() => onLoadWorkspace(name)}>
              <span className="ws-sidebar-icon">{icon}</span> {name}
            </div>
          ))}
        </div>

        <div className="ws-main">
          {wsEditing ? (
            <div className="ws-editor">
              <div className="ws-editor-header">
                <span className="ws-editor-filename">{wsEditing.path.split("/").pop()}{wsEditing.dirty ? " *" : ""}</span>
                <div className="ws-editor-actions">
                  <button
                    className="ws-tb-btn"
                    disabled={!wsEditing.dirty}
                    onClick={async () => {
                      try {
                        const response = await WorkspaceWriteFile(wsEditing.path, wsEditing.content);
                        if (response.error) {
                          onSetWsError(response.error);
                          return;
                        }
                        onSetWsEditing((previous) => previous ? { ...previous, dirty: false } : null);
                        onLoadWorkspace(wsPath);
                      } catch (errorValue) {
                        onSetWsError(String(errorValue));
                      }
                    }}
                  >
                    Save
                  </button>
                  <button
                    className="ws-tb-btn"
                    onClick={onRequestCloseEditor}
                  >
                    Close
                  </button>
                </div>
              </div>
              <textarea
                className="ws-editor-body"
                value={wsEditing.content}
                onChange={(event) => onSetWsEditing((previous) => previous ? { ...previous, content: event.target.value, dirty: true } : null)}
                spellCheck={false}
              />
            </div>
          ) : (
            <div className="ws-content">
              {wsNewName && (
                <div className="ws-entry ws-new-entry">
                  <span className="ws-entry-icon">{wsNewName.type === "dir" ? "\u{1F4C1}" : "\u{1F4C4}"}</span>
                  <input
                    className="ws-rename-input"
                    value={wsNewNameValue}
                    autoFocus
                    onChange={(event) => onSetWsNewNameValue(event.target.value)}
                    onKeyDown={async (event) => {
                      if (event.key === "Enter" && wsNewNameValue.trim()) {
                        const filePath = wsPath ? `${wsPath}/${wsNewNameValue.trim()}` : wsNewNameValue.trim();
                        try {
                          if (wsNewName.type === "dir") {
                            const response = await WorkspaceCreateDir(filePath);
                            if (response.error) onSetWsError(response.error);
                          } else {
                            const response = await WorkspaceWriteFile(filePath, "");
                            if (response.error) onSetWsError(response.error);
                          }
                          onSetWsNewName(null);
                          onSetWsNewNameValue("");
                          onLoadWorkspace(wsPath);
                        } catch (errorValue) {
                          onSetWsError(String(errorValue));
                        }
                      } else if (event.key === "Escape") {
                        onSetWsNewName(null);
                        onSetWsNewNameValue("");
                      }
                    }}
                    onBlur={() => {
                      onSetWsNewName(null);
                      onSetWsNewNameValue("");
                    }}
                    placeholder={wsNewName.type === "dir" ? "folder name" : "file name"}
                  />
                </div>
              )}

              {wsEntries.length > 0 && (
                <div className="ws-entry ws-entry-header">
                  <span className="ws-entry-icon" />
                  <span className="ws-entry-name">Name</span>
                  <span className="ws-entry-date">Modified</span>
                  <span className="ws-entry-size">Size</span>
                  <span className="ws-entry-actions" />
                </div>
              )}

              {wsLoading ? (
                <div className="ws-empty">Loading...</div>
              ) : (
                <>
                  {wsEntries.map((entry) => {
                    const filePath = wsPath ? `${wsPath}/${entry.name}` : entry.name;
                    const isDirectory = entry.entry_type === "directory";

                    return (
                      <div
                        key={entry.name}
                        className="ws-entry"
                        onClick={() => {
                          if (isDirectory) {
                            onLoadWorkspace(filePath);
                          } else if (isPreviewable(entry.name)) {
                            onSetPreview({ path: filePath, content: "", truncated: false, loading: true, error: "" });
                            WorkspacePreviewFile(filePath).then((response) => {
                              if (response.error) {
                                onSetPreview((previous) => previous ? { ...previous, loading: false, error: response.error || "" } : null);
                              } else {
                                onSetPreview({ path: filePath, content: response.content, truncated: response.truncated, loading: false, error: "" });
                              }
                            }).catch((errorValue) => onSetPreview((previous) => previous ? { ...previous, loading: false, error: String(errorValue) } : null));
                          }
                        }}
                        onDoubleClick={() => {
                          if (isDirectory) return;
                          if (isPreviewable(entry.name)) {
                            onSetPreview(null);
                            WorkspacePreviewFile(filePath).then((response) => {
                              if (response.error) {
                                onSetWsError(response.error || "Cannot open file");
                                return;
                              }
                              onSetWsEditing({ path: filePath, content: response.content, dirty: false });
                            }).catch((errorValue) => onSetWsError(String(errorValue)));
                          }
                        }}
                      >
                        <span className="ws-entry-icon">{isDirectory ? (WORKSPACE_ICONS[entry.name] || "\u{1F4C1}") : "\u{1F4C4}"}</span>
                        {wsRenaming === filePath ? (
                          <input
                            className="ws-rename-input"
                            value={wsRenameValue}
                            autoFocus
                            onClick={(event) => event.stopPropagation()}
                            onChange={(event) => onSetWsRenameValue(event.target.value)}
                            onKeyDown={async (event) => {
                              event.stopPropagation();
                              if (event.key === "Enter" && wsRenameValue.trim()) {
                                try {
                                  const response = await WorkspaceRename(filePath, wsRenameValue.trim());
                                  if (response.error) onSetWsError(response.error);
                                } catch (errorValue) {
                                  onSetWsError(String(errorValue));
                                }
                                onSetWsRenaming(null);
                                onLoadWorkspace(wsPath);
                              } else if (event.key === "Escape") {
                                onSetWsRenaming(null);
                              }
                            }}
                            onBlur={() => onSetWsRenaming(null)}
                          />
                        ) : (
                          <span className="ws-entry-name">{entry.name}</span>
                        )}
                        <span className="ws-entry-date">{entry.mod_time_utc ? formatTime(entry.mod_time_utc) : ""}</span>
                        <span className="ws-entry-size">{!isDirectory ? formatSize(entry.size_bytes) : "--"}</span>
                        <span className="ws-entry-actions">
                          <button className="ws-act-btn" title="Rename" onClick={(event) => { event.stopPropagation(); onSetWsRenaming(filePath); onSetWsRenameValue(entry.name); }}>{"\u270F"}</button>
                          <button
                            className="ws-act-btn"
                            title="Delete"
                            onClick={(event) => {
                              event.stopPropagation();
                              onRequestDeleteEntry(filePath, entry.name);
                            }}
                          >
                            {"\u{1F5D1}"}
                          </button>
                          {!isDirectory && (
                            <>
                              <button className="ws-review-btn" title="Review" onClick={(event) => { event.stopPropagation(); onRequestReviewPath(filePath); }}>Review</button>
                              <button className="ws-act-btn" title="Export" onClick={(event) => { event.stopPropagation(); onExportPath(filePath); }}>{"\u{1F4E4}"}</button>
                            </>
                          )}
                        </span>
                      </div>
                    );
                  })}
                  {wsEntries.length === 0 && !wsNewName && <div className="ws-empty">Empty folder</div>}
                </>
              )}
            </div>
          )}

          {preview && !wsEditing && (
            <div className="ws-preview">
              <div className="ws-preview-header">
                <span>{preview.path.split("/").pop()}</span>
                <div style={{ display: "flex", gap: "4px", alignItems: "center" }}>
                  {!preview.loading && !preview.error && (
                    <button className="ws-tb-btn" onClick={() => onRequestReviewPath(preview.path)}>Review</button>
                  )}
                  {!preview.loading && !preview.error && isPreviewable(preview.path) && (
                    <button className="ws-tb-btn" onClick={() => { onSetWsEditing({ path: preview.path, content: preview.content, dirty: false }); onSetPreview(null); }}>Edit</button>
                  )}
                  <button className="ws-preview-close" onClick={() => onSetPreview(null)}>&times;</button>
                </div>
              </div>
              {preview.loading ? (
                <div className="ws-preview-body">Loading...</div>
              ) : preview.error ? (
                <div className="ws-preview-body" style={{ color: "var(--red)" }}>{preview.error}</div>
              ) : (
                <pre className="ws-preview-body">{preview.content}{preview.truncated && <span className="ws-preview-truncated">{"\n--- truncated ---"}</span>}</pre>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

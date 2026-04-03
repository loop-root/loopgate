import { useEffect, useState } from "react";

import type {
  WorkingNoteResponse,
  WorkingNoteSaveRequest,
  WorkingNoteSummary,
} from "../../../wailsjs/go/main/HavenApp";

interface NotesWindowProps {
  notes: WorkingNoteSummary[];
  selectedPath: string | null;
  activeNote: WorkingNoteResponse | null;
  loadingList: boolean;
  loadingNote: boolean;
  savingNote: boolean;
  onRefresh: () => void;
  onSelectNote: (path: string) => void;
  onCreateNote: () => void;
  onSaveNote: (request: WorkingNoteSaveRequest) => Promise<void>;
}

export default function NotesWindow({
  notes,
  selectedPath,
  activeNote,
  loadingList,
  loadingNote,
  savingNote,
  onRefresh,
  onSelectNote,
  onCreateNote,
  onSaveNote,
}: NotesWindowProps) {
  const [draftTitle, setDraftTitle] = useState("");
  const [draftContent, setDraftContent] = useState("");
  const [savingError, setSavingError] = useState("");

  useEffect(() => {
    setDraftTitle(activeNote?.title || "");
    setDraftContent(activeNote?.content || "");
    setSavingError("");
  }, [activeNote?.path, activeNote?.title, activeNote?.content]);

  const currentPath = activeNote?.path || "";
  const isDirty = draftTitle !== (activeNote?.title || "") || draftContent !== (activeNote?.content || "");

  return (
    <div className="notes-app">
      <div className="app-hero notes-hero">
        <div className="app-kicker">Notes</div>
        <div className="app-title">Morph's working notebook for plans, scratch work, and research fragments.</div>
        <div className="app-subtitle">
          Use this room for operational thoughts that should persist in Haven without becoming a journal entry or a public sticky note.
        </div>
        <div className="app-summary-pills">
          <span className="app-summary-pill">{notes.length} note{notes.length === 1 ? "" : "s"}</span>
          <span className="app-summary-pill">{currentPath ? "editing" : "drafting"}</span>
          {isDirty && <span className="app-summary-pill">unsaved</span>}
        </div>
      </div>

      <div className="notes-sidebar">
        <div className="notes-sidebar-header">
          <div>
            <div className="notes-sidebar-title">Notebook</div>
            <div className="notes-sidebar-subtitle">Morph's private working memory</div>
          </div>
          <button className="ws-tb-btn" onClick={onRefresh} title="Refresh">↻</button>
        </div>

        <button
          className="notes-new-btn"
          onClick={() => {
            onCreateNote();
            setDraftTitle("");
            setDraftContent("");
            setSavingError("");
          }}
        >
          New Note
        </button>

        <div className="notes-entry-list">
          {loadingList ? (
            <div className="notes-empty">Loading notes...</div>
          ) : notes.length === 0 ? (
            <div className="notes-empty">
              <div className="notes-empty-title">No notes yet</div>
              <div className="notes-empty-copy">When Morph starts planning more explicitly, those traces will start to collect here.</div>
            </div>
          ) : (
            notes.map((note) => (
              <div
                key={note.path}
                className={`notes-list-entry ${selectedPath === note.path ? "active" : ""}`}
                onClick={() => onSelectNote(note.path)}
              >
                <div className="notes-list-title">{note.title}</div>
                <div className="notes-list-preview">{note.preview}</div>
              </div>
            ))
          )}
        </div>
      </div>

      <div className="notes-main">
        {loadingNote ? (
          <div className="notes-editor-empty">Opening note...</div>
        ) : (
          <>
            <div className="notes-editor-header">
              <input
                className="notes-title-input"
                value={draftTitle}
                onChange={(event) => setDraftTitle(event.target.value)}
                placeholder="Untitled note"
                maxLength={80}
              />
              <button
                className="notes-save-btn"
                disabled={savingNote || !draftContent.trim()}
                onClick={async () => {
                  try {
                    setSavingError("");
                    await onSaveNote({
                      path: currentPath || undefined,
                      title: draftTitle.trim() || undefined,
                      content: draftContent,
                    });
                  } catch (errorValue) {
                    setSavingError(String(errorValue));
                  }
                }}
              >
                {savingNote ? "Saving..." : "Save"}
              </button>
            </div>
            {savingError && <div className="notes-error">{savingError}</div>}
            <textarea
              className="notes-editor-body"
              value={draftContent}
              onChange={(event) => setDraftContent(event.target.value)}
              placeholder="Write a plan, scratchpad, or working note for Morph..."
              spellCheck={false}
            />
            {!draftContent.trim() && (
              <div className="notes-editor-empty">
                Start a working note here. Use it for plans, questions, or partial reasoning that should stay inside Haven.
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

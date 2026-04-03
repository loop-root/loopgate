import { useCallback, useEffect, useState } from "react";

import type { WorkingNoteResponse, WorkingNoteSummary } from "../../wailsjs/go/main/HavenApp";
import { ListWorkingNotes, ReadWorkingNote, SaveWorkingNote } from "../../wailsjs/go/main/HavenApp";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { NEW_WORKING_NOTE_PATH } from "../app/havenTypes";
import type { Toast } from "../lib/haven";

export function useWorkingNotesWindowState(options: {
  notesWindowOpen: boolean;
  pushToast: (title: string, message: string, variant?: Toast["variant"]) => void;
}) {
  const { notesWindowOpen, pushToast } = options;

  const [workingNotes, setWorkingNotes] = useState<WorkingNoteSummary[]>([]);
  const [activeWorkingNotePath, setActiveWorkingNotePath] = useState<string | null>(null);
  const [activeWorkingNote, setActiveWorkingNote] = useState<WorkingNoteResponse | null>(null);
  const [workingNotesLoading, setWorkingNotesLoading] = useState(false);
  const [workingNoteLoading, setWorkingNoteLoading] = useState(false);
  const [workingNoteSaving, setWorkingNoteSaving] = useState(false);

  const loadWorkingNote = useCallback(async (path: string) => {
    setWorkingNoteLoading(true);
    try {
      const response = await ReadWorkingNote(path);
      if (response.error) {
        setActiveWorkingNote(null);
        pushToast("Notes unavailable", response.error, "warning");
        return;
      }
      setActiveWorkingNote(response);
    } catch (errorValue) {
      setActiveWorkingNote(null);
      pushToast("Notes unavailable", String(errorValue), "warning");
    } finally {
      setWorkingNoteLoading(false);
    }
  }, [pushToast]);

  const refreshWorkingNotes = useCallback(async (preferredPath?: string | null) => {
    setWorkingNotesLoading(true);
    try {
      const notes = await ListWorkingNotes();
      setWorkingNotes(notes || []);

      let nextPath = preferredPath ?? activeWorkingNotePath;
      if (nextPath === NEW_WORKING_NOTE_PATH) {
        setActiveWorkingNotePath(NEW_WORKING_NOTE_PATH);
        setActiveWorkingNote({ path: "", title: "", content: "" });
        return;
      }
      if (!nextPath || !(notes || []).some((note) => note.path === nextPath)) {
        nextPath = notes?.[0]?.path || null;
      }

      setActiveWorkingNotePath(nextPath);
      if (nextPath) {
        await loadWorkingNote(nextPath);
      } else {
        setActiveWorkingNote(null);
      }
    } catch (errorValue) {
      setWorkingNotes([]);
      setActiveWorkingNote(null);
      pushToast("Notes unavailable", String(errorValue), "warning");
    } finally {
      setWorkingNotesLoading(false);
    }
  }, [activeWorkingNotePath, loadWorkingNote, pushToast]);

  useEffect(() => {
    if (notesWindowOpen) {
      void refreshWorkingNotes(activeWorkingNotePath);
    }
  }, [activeWorkingNotePath, notesWindowOpen, refreshWorkingNotes]);

  useEffect(() => {
    const unsubscribe = EventsOn("haven:file_changed", (...args: unknown[]) => {
      if (!notesWindowOpen) return;
      const data = (args[0] as Record<string, unknown>) || {};
      const action = String(data.action || "");
      const path = String(data.path || "");
      if (action === "notes_write" || path.startsWith("scratch/notes/") || path.startsWith("research/notes/")) {
        void refreshWorkingNotes(activeWorkingNotePath);
      }
    });
    return () => { unsubscribe(); };
  }, [activeWorkingNotePath, notesWindowOpen, refreshWorkingNotes]);

  const handleSaveWorkingNote = useCallback(async (request: { path?: string; title?: string; content: string }) => {
    setWorkingNoteSaving(true);
    try {
      const response = await SaveWorkingNote(request);
      if (response.error) {
        pushToast("Notes", response.error, "warning");
        throw new Error(response.error);
      }
      if (response.path) {
        setActiveWorkingNotePath(response.path);
        setActiveWorkingNote({
          path: response.path,
          title: response.title || request.title || "Untitled Note",
          content: request.content,
        });
      }
      await refreshWorkingNotes(response.path || activeWorkingNotePath);
      pushToast("Notes", "Saved to Morph's notebook.", "success");
    } finally {
      setWorkingNoteSaving(false);
    }
  }, [activeWorkingNotePath, pushToast, refreshWorkingNotes]);

  return {
    workingNotes,
    activeWorkingNotePath,
    setActiveWorkingNotePath,
    activeWorkingNote,
    setActiveWorkingNote,
    workingNotesLoading,
    workingNoteLoading,
    workingNoteSaving,
    refreshWorkingNotes,
    loadWorkingNote,
    handleSaveWorkingNote,
  };
}

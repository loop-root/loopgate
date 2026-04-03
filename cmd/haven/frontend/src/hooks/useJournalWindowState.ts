import { useCallback, useEffect, useState } from "react";

import type { JournalEntryResponse, JournalEntrySummary } from "../../wailsjs/go/main/HavenApp";
import { ListJournalEntries, ReadJournalEntry } from "../../wailsjs/go/main/HavenApp";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { Toast } from "../lib/haven";

export function useJournalWindowState(options: {
  journalWindowOpen: boolean;
  pushToast: (title: string, message: string, variant?: Toast["variant"]) => void;
}) {
  const { journalWindowOpen, pushToast } = options;

  const [journalEntries, setJournalEntries] = useState<JournalEntrySummary[]>([]);
  const [activeJournalPath, setActiveJournalPath] = useState<string | null>(null);
  const [activeJournalEntry, setActiveJournalEntry] = useState<JournalEntryResponse | null>(null);
  const [journalLoading, setJournalLoading] = useState(false);
  const [journalEntryLoading, setJournalEntryLoading] = useState(false);

  const loadJournalEntry = useCallback(async (path: string) => {
    setJournalEntryLoading(true);
    try {
      const response = await ReadJournalEntry(path);
      if (response.error) {
        setActiveJournalEntry(null);
        pushToast("Journal unavailable", response.error, "warning");
        return;
      }
      setActiveJournalEntry(response);
    } catch (errorValue) {
      setActiveJournalEntry(null);
      pushToast("Journal unavailable", String(errorValue), "warning");
    } finally {
      setJournalEntryLoading(false);
    }
  }, [pushToast]);

  const refreshJournalEntries = useCallback(async (preferredPath?: string | null) => {
    setJournalLoading(true);
    try {
      const entries = await ListJournalEntries();
      setJournalEntries(entries || []);

      let nextPath = preferredPath ?? activeJournalPath;
      if (!nextPath || !(entries || []).some((entry) => entry.path === nextPath)) {
        nextPath = entries?.[0]?.path || null;
      }

      setActiveJournalPath(nextPath);
      if (nextPath) {
        await loadJournalEntry(nextPath);
      } else {
        setActiveJournalEntry(null);
      }
    } catch (errorValue) {
      setJournalEntries([]);
      setActiveJournalEntry(null);
      pushToast("Journal unavailable", String(errorValue), "warning");
    } finally {
      setJournalLoading(false);
    }
  }, [activeJournalPath, loadJournalEntry, pushToast]);

  useEffect(() => {
    if (journalWindowOpen) {
      void refreshJournalEntries(activeJournalPath);
    }
  }, [activeJournalPath, journalWindowOpen, refreshJournalEntries]);

  useEffect(() => {
    const unsubscribe = EventsOn("haven:file_changed", (...args: unknown[]) => {
      if (!journalWindowOpen) return;
      const data = (args[0] as Record<string, unknown>) || {};
      const path = String(data.path || "");
      if (path.startsWith("scratch/journal/") || path.startsWith("research/journal/")) {
        void refreshJournalEntries(activeJournalPath);
      }
    });
    return () => { unsubscribe(); };
  }, [activeJournalPath, journalWindowOpen, refreshJournalEntries]);

  return {
    journalEntries,
    activeJournalPath,
    setActiveJournalPath,
    activeJournalEntry,
    journalLoading,
    journalEntryLoading,
    refreshJournalEntries,
    loadJournalEntry,
  };
}

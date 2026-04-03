import { useEffect, useState } from "react";

import type { DeskNote } from "../../wailsjs/go/main/HavenApp";
import { ListDeskNotes } from "../../wailsjs/go/main/HavenApp";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export function useDeskNotesSync(booting: boolean) {
  const [deskNotes, setDeskNotes] = useState<DeskNote[]>([]);

  useEffect(() => {
    if (booting) return;
    const refreshNotes = () => { ListDeskNotes().then((notes) => setDeskNotes(notes || [])).catch(() => {}); };
    refreshNotes();
    const unsubscribe = EventsOn("haven:desk_notes_changed", () => refreshNotes());
    return () => { unsubscribe(); };
  }, [booting]);

  return { deskNotes, setDeskNotes };
}

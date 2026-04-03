import { useEffect, useRef, useState } from "react";

import { DRAG_THRESHOLD } from "../app/havenTypes";
import type { PendingFileDragState } from "../app/havenTypes";

export function useDeskNoteDrag() {
  const [deskNotePositions, setDeskNotePositions] = useState<Record<string, { x: number; y: number }>>(() => {
    try {
      const saved = localStorage.getItem("haven-desknote-positions");
      if (saved) return JSON.parse(saved);
    } catch {
      // Ignore invalid local storage state.
    }
    return {};
  });

  const [draggingNote, setDraggingNote] = useState<string | null>(null);
  const noteDragOffset = useRef({ x: 0, y: 0 });
  const [pendingNoteDrag, setPendingNoteDrag] = useState<PendingFileDragState | null>(null);

  useEffect(() => {
    if (!draggingNote && !pendingNoteDrag) return;

    if (pendingNoteDrag && !draggingNote) {
      const pendingDrag = pendingNoteDrag;
      const handleMove = (event: MouseEvent) => {
        const dx = event.clientX - pendingDrag.startX;
        const dy = event.clientY - pendingDrag.startY;
        if (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD) {
          setDraggingNote(pendingDrag.id);
          setPendingNoteDrag(null);
        }
      };
      const handleUp = () => setPendingNoteDrag(null);
      document.addEventListener("mousemove", handleMove);
      document.addEventListener("mouseup", handleUp);
      return () => {
        document.removeEventListener("mousemove", handleMove);
        document.removeEventListener("mouseup", handleUp);
      };
    }

    const handleMove = (event: MouseEvent) => {
      setDeskNotePositions((previous) => ({
        ...previous,
        [draggingNote!]: { x: event.clientX - noteDragOffset.current.x, y: event.clientY - noteDragOffset.current.y },
      }));
    };
    const handleUp = () => {
      setDraggingNote(null);
      setDeskNotePositions((previous) => {
        try {
          localStorage.setItem("haven-desknote-positions", JSON.stringify(previous));
        } catch {
          // Ignore local storage write failures.
        }
        return previous;
      });
    };

    document.addEventListener("mousemove", handleMove);
    document.addEventListener("mouseup", handleUp);
    return () => {
      document.removeEventListener("mousemove", handleMove);
      document.removeEventListener("mouseup", handleUp);
    };
  }, [draggingNote, pendingNoteDrag]);

  return { deskNotePositions, setDeskNotePositions, noteDragOffset, setPendingNoteDrag };
}

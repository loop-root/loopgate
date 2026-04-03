import { useEffect, useRef, useState } from "react";

import type { PendingFileDragState } from "../app/havenTypes";
import { DRAG_THRESHOLD } from "../app/havenTypes";
import type { DesktopFile } from "../lib/haven";

export function useDesktopFileDrag() {
  const [desktopFiles, setDesktopFiles] = useState<DesktopFile[]>(() => {
    try {
      const saved = localStorage.getItem("haven-desktop-files");
      if (saved) return JSON.parse(saved);
    } catch {
      // Ignore invalid local storage state.
    }
    return [];
  });
  const [draggingFile, setDraggingFile] = useState<string | null>(null);
  const fileDragOffset = useRef({ x: 0, y: 0 });
  const [pendingFileDrag, setPendingFileDrag] = useState<PendingFileDragState | null>(null);

  useEffect(() => {
    if (!draggingFile && !pendingFileDrag) return;

    if (pendingFileDrag && !draggingFile) {
      const pendingDrag = pendingFileDrag;
      const handleMove = (event: MouseEvent) => {
        const dx = event.clientX - pendingDrag.startX;
        const dy = event.clientY - pendingDrag.startY;
        if (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD) {
          setDraggingFile(pendingDrag.id);
          setPendingFileDrag(null);
        }
      };
      const handleUp = () => setPendingFileDrag(null);
      document.addEventListener("mousemove", handleMove);
      document.addEventListener("mouseup", handleUp);
      return () => {
        document.removeEventListener("mousemove", handleMove);
        document.removeEventListener("mouseup", handleUp);
      };
    }

    const handleMove = (event: MouseEvent) => {
      setDesktopFiles((previous) => previous.map((file) => (
        file.id === draggingFile
          ? { ...file, x: event.clientX - fileDragOffset.current.x, y: event.clientY - fileDragOffset.current.y }
          : file
      )));
    };
    const handleUp = () => {
      setDraggingFile(null);
      setDesktopFiles((previous) => {
        try {
          localStorage.setItem("haven-desktop-files", JSON.stringify(previous));
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
  }, [draggingFile, pendingFileDrag]);

  return { desktopFiles, setDesktopFiles, fileDragOffset, setPendingFileDrag };
}

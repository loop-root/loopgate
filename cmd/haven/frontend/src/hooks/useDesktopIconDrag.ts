import { useEffect, useRef, useState } from "react";

import type { PendingIconDragState } from "../app/havenTypes";
import { DRAG_THRESHOLD } from "../app/havenTypes";
import type { AppID, IconPos } from "../lib/haven";

export function useDesktopIconDrag() {
  const [iconPositions, setIconPositions] = useState<Record<string, IconPos>>(() => {
    const defaults: Record<string, IconPos> = {
      morph: { x: -110, y: 20 },
      loopgate: { x: -110, y: 104 },
      workspace: { x: -210, y: 20 },
      todo: { x: -210, y: 188 },
      notes: { x: -110, y: 272 },
      journal: { x: -210, y: 272 },
      paint: { x: -110, y: 188 },
    };
    try {
      const saved = localStorage.getItem("haven-icon-positions");
      if (saved) return { ...defaults, ...JSON.parse(saved) };
    } catch {
      // Ignore invalid local storage state.
    }
    return defaults;
  });
  const [draggingIcon, setDraggingIcon] = useState<AppID | null>(null);
  const iconDragOffset = useRef({ x: 0, y: 0 });
  const [pendingIconDrag, setPendingIconDrag] = useState<PendingIconDragState | null>(null);

  useEffect(() => {
    if (!draggingIcon && !pendingIconDrag) return;

    if (pendingIconDrag && !draggingIcon) {
      const pendingDrag = pendingIconDrag;
      const handleMove = (event: MouseEvent) => {
        const dx = event.clientX - pendingDrag.startX;
        const dy = event.clientY - pendingDrag.startY;
        if (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD) {
          setIconPositions((previous) => ({ ...previous, [pendingDrag.id]: { x: pendingDrag.rectX, y: pendingDrag.rectY } }));
          setDraggingIcon(pendingDrag.id);
          setPendingIconDrag(null);
        }
      };
      const handleUp = () => setPendingIconDrag(null);
      document.addEventListener("mousemove", handleMove);
      document.addEventListener("mouseup", handleUp);
      return () => {
        document.removeEventListener("mousemove", handleMove);
        document.removeEventListener("mouseup", handleUp);
      };
    }

    const handleMove = (event: MouseEvent) => {
      setIconPositions((previous) => ({
        ...previous,
        [draggingIcon!]: { x: event.clientX - iconDragOffset.current.x, y: event.clientY - iconDragOffset.current.y },
      }));
    };
    const handleUp = () => {
      setDraggingIcon(null);
      setIconPositions((previous) => {
        try {
          localStorage.setItem("haven-icon-positions", JSON.stringify(previous));
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
  }, [draggingIcon, pendingIconDrag]);

  return { iconPositions, setIconPositions, iconDragOffset, setPendingIconDrag };
}

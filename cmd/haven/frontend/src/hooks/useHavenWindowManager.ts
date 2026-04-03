import { useCallback, useRef, useState } from "react";

import {
  resolveWindowFrame,
  type AppID,
  type WinState,
} from "../lib/haven";
import { finiteWindowValue } from "../app/havenTypes";

export function useHavenWindowManager() {
  const [windows, setWindows] = useState<Record<string, WinState>>({});
  const [focusedWin, setFocusedWin] = useState<string | null>(null);
  const nextZ = useRef(100);

  const openWindow = useCallback((id: AppID) => {
    setWindows((previous) => {
      if (previous[id]) {
        const z = ++nextZ.current;
        return { ...previous, [id]: { ...previous[id], collapsed: false, z } };
      }

      const z = ++nextZ.current;
      let savedFrame: Partial<Pick<WinState, "x" | "y" | "w" | "h">> = {};

      try {
        const saved = localStorage.getItem(`haven-win-${id}`);
        if (saved) {
          const parsed = JSON.parse(saved);
          savedFrame = {
            x: finiteWindowValue(parsed.x),
            y: finiteWindowValue(parsed.y),
            w: finiteWindowValue(parsed.w),
            h: finiteWindowValue(parsed.h),
          };
        }
      } catch {
        // Ignore invalid local storage state.
      }

      const frame = resolveWindowFrame(id, savedFrame);
      return { ...previous, [id]: { id, title: frame.title, x: frame.x, y: frame.y, w: frame.w, h: frame.h, z, collapsed: false } };
    });
    setFocusedWin(id);
  }, []);

  const closeWindow = useCallback((id: string) => {
    setWindows((previous) => {
      const win = previous[id];
      if (win) {
        try {
          localStorage.setItem(`haven-win-${id}`, JSON.stringify({ x: win.x, y: win.y, w: win.w, h: win.h }));
        } catch {
          // Ignore local storage write failures.
        }
      }
      const next = { ...previous };
      delete next[id];
      return next;
    });
    setFocusedWin((current) => (current === id ? null : current));
  }, []);

  const focusWindow = useCallback((id: string) => {
    const z = ++nextZ.current;
    setWindows((previous) => (previous[id] ? { ...previous, [id]: { ...previous[id], z } } : previous));
    setFocusedWin(id);
  }, []);

  const collapseWindow = useCallback((id: string) => {
    setWindows((previous) => (previous[id] ? { ...previous, [id]: { ...previous[id], collapsed: !previous[id].collapsed } } : previous));
  }, []);

  const dragWindow = useCallback((id: string, x: number, y: number) => {
    setWindows((previous) => (previous[id] ? { ...previous, [id]: { ...previous[id], x, y } } : previous));
  }, []);

  const resizeWindow = useCallback((id: string, x: number, y: number, w: number, h: number) => {
    setWindows((previous) => (previous[id] ? { ...previous, [id]: { ...previous[id], x, y, w, h } } : previous));
  }, []);

  return {
    windows,
    setWindows,
    focusedWin,
    setFocusedWin,
    nextZ,
    openWindow,
    closeWindow,
    focusWindow,
    collapseWindow,
    dragWindow,
    resizeWindow,
  };
}

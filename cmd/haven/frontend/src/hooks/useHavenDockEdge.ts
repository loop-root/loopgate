import { useCallback, useState } from "react";

const STORAGE_KEY = "haven-dock-edge";

export type HavenDockEdge = "bottom" | "left" | "right";

export function useHavenDockEdge() {
  const [dockEdge, setDockEdgeState] = useState<HavenDockEdge>(() => {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw === "left" || raw === "right" || raw === "bottom") return raw;
    } catch {
      // Ignore invalid local storage state.
    }
    return "bottom";
  });

  const setDockEdge = useCallback((next: HavenDockEdge) => {
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // Ignore local storage write failures.
    }
    setDockEdgeState(next);
  }, []);

  return { dockEdge, setDockEdge };
}

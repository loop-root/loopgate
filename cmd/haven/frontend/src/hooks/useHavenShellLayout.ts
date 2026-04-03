import { useCallback, useState } from "react";

const STORAGE_KEY = "haven-shell-layout";

export type HavenShellLayout = "workstation" | "classic";

export function useHavenShellLayout() {
  const [shellLayout, setShellLayoutState] = useState<HavenShellLayout>(() => {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw === "classic") return "classic";
      if (raw === "workstation") return "workstation";
    } catch {
      // Ignore invalid local storage state.
    }
    return "workstation";
  });

  const setShellLayout = useCallback((next: HavenShellLayout) => {
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // Ignore local storage write failures.
    }
    setShellLayoutState(next);
  }, []);

  return { shellLayout, setShellLayout };
}

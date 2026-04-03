import { useCallback, useState } from "react";

const STORAGE_KEY = "haven-workstation-workspace-open";

/**
 * Whether the embedded workspace column is visible in workstation layout.
 * Default false so the desk (notes, wallpaper) reads as primary until the user
 * opens workspace or a file/import path surfaces it.
 */
export function useWorkstationWorkspaceOpen() {
  const [workspaceOpen, setWorkspaceOpenState] = useState<boolean>(() => {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw === "1" || raw === "true") return true;
      if (raw === "0" || raw === "false") return false;
    } catch {
      // Ignore invalid local storage state.
    }
    return false;
  });

  const setWorkspaceOpen = useCallback((next: boolean | ((previous: boolean) => boolean)) => {
    setWorkspaceOpenState((previous) => {
      const resolved = typeof next === "function" ? (next as (p: boolean) => boolean)(previous) : next;
      try {
        localStorage.setItem(STORAGE_KEY, resolved ? "1" : "0");
      } catch {
        // Ignore local storage write failures.
      }
      return resolved;
    });
  }, []);

  return { workstationWorkspaceOpen: workspaceOpen, setWorkstationWorkspaceOpen: setWorkspaceOpen };
}

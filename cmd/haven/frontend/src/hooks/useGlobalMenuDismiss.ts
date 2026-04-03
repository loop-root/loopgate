import { useEffect, type Dispatch, type SetStateAction } from "react";

import type { ContextMenuState } from "../lib/haven";

export function useGlobalMenuDismiss(
  setContextMenu: Dispatch<SetStateAction<ContextMenuState | null>>,
  setOpenMenu: Dispatch<SetStateAction<string | null>>,
) {
  useEffect(() => {
    const handleClick = () => {
      setContextMenu(null);
      setOpenMenu(null);
    };
    document.addEventListener("click", handleClick);
    return () => document.removeEventListener("click", handleClick);
  }, [setContextMenu, setOpenMenu]);
}

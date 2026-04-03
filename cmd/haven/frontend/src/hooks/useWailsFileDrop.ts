import { useEffect } from "react";

import type { PendingDrop } from "../lib/haven";
import { OnFileDrop, OnFileDropOff } from "../../wailsjs/runtime/runtime";

export function useWailsFileDrop(
  setDropActive: (active: boolean) => void,
  setPendingDrop: (drop: PendingDrop | null) => void,
) {
  useEffect(() => {
    OnFileDrop((x: number, y: number, paths: string[]) => {
      setDropActive(false);
      if (!paths?.length) return;
      setPendingDrop({ paths, x, y: Math.max(y, 40) });
    }, true);
    return () => { OnFileDropOff(); };
  }, [setDropActive, setPendingDrop]);
}

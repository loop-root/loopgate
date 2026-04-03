import { useEffect, type Dispatch, type SetStateAction } from "react";

import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { IconPos } from "../lib/haven";

export function useRemoteIconPositionsSync(
  booting: boolean,
  setIconPositions: Dispatch<SetStateAction<Record<string, IconPos>>>,
) {
  useEffect(() => {
    if (booting) return;
    const unsubscribe = EventsOn("haven:icon_positions_changed", (...args: unknown[]) => {
      const data = args[0] as Record<string, unknown>;
      const positions = data.positions as Record<string, IconPos> | undefined;
      if (positions && typeof positions === "object") {
        setIconPositions((previous) => {
          const next = { ...previous, ...positions };
          try { localStorage.setItem("haven-icon-positions", JSON.stringify(next)); } catch { /* ignore */ }
          return next;
        });
      }
    });
    return () => { unsubscribe(); };
  }, [booting, setIconPositions]);
}

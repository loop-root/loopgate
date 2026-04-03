import { useEffect } from "react";

import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { Toast } from "../lib/haven";

export function useHavenBackendToastBridge(
  booting: boolean,
  pushToast: (title: string, message: string, variant?: Toast["variant"]) => void,
) {
  useEffect(() => {
    if (booting) return;
    const unsubscribe = EventsOn("haven:toast", (...args: unknown[]) => {
      const data = args[0] as Record<string, unknown>;
      pushToast(
        (data.title as string) || "",
        (data.message as string) || "",
        ((data.variant as string) || "info") as Toast["variant"],
      );
    });
    return () => { unsubscribe(); };
  }, [booting, pushToast]);
}

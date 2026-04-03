import { useEffect, useState } from "react";

import type { PresenceResponse } from "../../wailsjs/go/main/HavenApp";
import { GetPresence } from "../../wailsjs/go/main/HavenApp";
import { EventsOn } from "../../wailsjs/runtime/runtime";

const defaultPresence: PresenceResponse = {
  state: "idle",
  status_text: "Morph is getting settled in",
  detail_text: "finding a comfortable place in Haven",
  anchor: "desk",
};

export function useHavenPresence(booting: boolean): PresenceResponse {
  const [presence, setPresence] = useState<PresenceResponse>(defaultPresence);

  useEffect(() => {
    if (booting) return;
    GetPresence().then(setPresence).catch(() => {});
    const unsubscribe = EventsOn("haven:presence_changed", (...args: unknown[]) => {
      const data = args[0] as Record<string, unknown>;
      setPresence({
        state: (data.state as string) || "idle",
        status_text: (data.status_text as string) || "Morph is idle",
        detail_text: (data.detail_text as string) || "",
        anchor: (data.anchor as string) || "desk",
      });
    });
    return () => { unsubscribe(); };
  }, [booting]);

  return presence;
}

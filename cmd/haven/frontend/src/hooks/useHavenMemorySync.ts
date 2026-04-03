import { useEffect, useState } from "react";

import type { MemoryStatusResponse } from "../../wailsjs/go/main/HavenApp";
import { GetMemoryStatus } from "../../wailsjs/go/main/HavenApp";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export function useHavenMemorySync(booting: boolean) {
  const [memoryStatus, setMemoryStatus] = useState<MemoryStatusResponse | null>(null);
  const [memoryLoaded, setMemoryLoaded] = useState(false);

  useEffect(() => {
    if (booting) return;
    GetMemoryStatus().then((status) => {
      setMemoryStatus(status);
      setMemoryLoaded(true);
    }).catch(() => {
      setMemoryLoaded(true);
    });
    const unsubscribe = EventsOn("haven:memory_updated", (...args: unknown[]) => {
      const data = args[0] as MemoryStatusResponse;
      setMemoryStatus(data);
      setMemoryLoaded(true);
    });
    return () => { unsubscribe(); };
  }, [booting]);

  return { memoryStatus, memoryLoaded };
}

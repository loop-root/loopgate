import { useEffect, useState } from "react";

import type { SystemStatusResponse } from "../../wailsjs/go/main/HavenApp";
import { SystemStatus } from "../../wailsjs/go/main/HavenApp";

export function useSystemStatusPoll(booting: boolean) {
  const [systemStatus, setSystemStatus] = useState<SystemStatusResponse | null>(null);

  useEffect(() => {
    if (booting) return;
    const poll = () => { SystemStatus().then(setSystemStatus).catch(() => {}); };
    poll();
    const timer = setInterval(poll, 8000);
    return () => clearInterval(timer);
  }, [booting]);

  return { systemStatus, setSystemStatus };
}

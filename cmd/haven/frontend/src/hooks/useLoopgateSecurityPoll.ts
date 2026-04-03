import { useEffect } from "react";

export function useLoopgateSecurityPoll(loopgateOpen: boolean, reloadSecurityOverview: () => void) {
  useEffect(() => {
    if (!loopgateOpen) return;
    reloadSecurityOverview();
    const timer = setInterval(reloadSecurityOverview, 10000);
    return () => clearInterval(timer);
  }, [loopgateOpen, reloadSecurityOverview]);
}

import { useEffect, useState } from "react";

import { CheckSetup } from "../../wailsjs/go/main/HavenApp";
import { BOOT_LINES } from "../lib/haven";

export function useHavenBootSetup() {
  const [booting, setBooting] = useState(true);
  const [bootLines, setBootLines] = useState<string[]>([]);
  const [bootFading, setBootFading] = useState(false);
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null);

  useEffect(() => {
    let index = 0;
    const timer = setInterval(() => {
      if (index < BOOT_LINES.length) {
        setBootLines((previous) => [...previous, BOOT_LINES[index]]);
        index++;
        return;
      }

      clearInterval(timer);
      setTimeout(() => setBootFading(true), 400);
      setTimeout(() => {
        setBooting(false);
        CheckSetup().then((status) => setNeedsSetup(status.needs_setup)).catch(() => setNeedsSetup(false));
      }, 1200);
    }, 180);
    return () => clearInterval(timer);
  }, []);

  return { booting, bootLines, bootFading, needsSetup, setNeedsSetup };
}

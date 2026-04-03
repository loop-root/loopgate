import { useCallback, useRef, useState } from "react";

import type { Toast } from "../lib/haven";

export function useHavenToast() {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextToastID = useRef(0);

  const pushToast = useCallback((title: string, message: string, variant: Toast["variant"] = "info") => {
    const toastID = nextToastID.current++;
    const toast: Toast = { id: toastID, title, message, variant };
    setToasts((previous) => [...previous, toast]);
    setTimeout(() => setToasts((previous) => previous.filter((item) => item.id !== toastID)), 5000);
  }, []);

  return { toasts, setToasts, pushToast };
}

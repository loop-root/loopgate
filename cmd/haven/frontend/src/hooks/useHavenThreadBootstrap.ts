import { useEffect, type Dispatch, type SetStateAction } from "react";

import { ListThreads } from "../../wailsjs/go/main/HavenApp";
import type { ThreadSummary } from "../../wailsjs/go/main/HavenApp";

export function useHavenThreadBootstrap(options: {
  selectThread: (threadID: string) => void | Promise<void>;
  setThreads: Dispatch<SetStateAction<ThreadSummary[]>>;
  setThreadsLoaded: Dispatch<SetStateAction<boolean>>;
}) {
  const { selectThread, setThreads, setThreadsLoaded } = options;

  useEffect(() => {
    let cancelled = false;
    ListThreads().then((items) => {
      if (cancelled) return;
      const nextThreads = items || [];
      setThreads(nextThreads);
      setThreadsLoaded(true);
      if (nextThreads.length === 0) {
        return;
      }

      let preferredThreadID = "";
      try {
        preferredThreadID = localStorage.getItem("haven-active-thread-id") || "";
      } catch {
        preferredThreadID = "";
      }
      const initialThread = nextThreads.find((thread) => thread.thread_id === preferredThreadID) || nextThreads[0];
      if (initialThread) {
        void selectThread(initialThread.thread_id);
      }
    }).catch(() => {
      if (!cancelled) {
        setThreadsLoaded(true);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [selectThread, setThreads, setThreadsLoaded]);
}

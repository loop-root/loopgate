import { useEffect } from "react";

import { EventsOn } from "../../wailsjs/runtime/runtime";

export function useWorkspaceWindowEffects(options: {
  workspaceOpen: boolean;
  wsPath: string;
  wsEntriesLength: number;
  wsLoading: boolean;
  loadWorkspace: (path: string) => void | Promise<void>;
}) {
  const { workspaceOpen, wsPath, wsEntriesLength, wsLoading, loadWorkspace } = options;

  useEffect(() => {
    if (workspaceOpen && wsEntriesLength === 0 && !wsLoading) loadWorkspace("");
  }, [loadWorkspace, wsEntriesLength, wsLoading, workspaceOpen]);

  useEffect(() => {
    const unsubscribe = EventsOn("haven:file_changed", () => {
      if (workspaceOpen) loadWorkspace(wsPath);
    });
    return () => { unsubscribe(); };
  }, [loadWorkspace, wsPath, workspaceOpen]);
}

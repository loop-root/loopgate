import { useEffect, type MutableRefObject, type Dispatch, type SetStateAction } from "react";

import type { ConversationEvent, ThreadSummary } from "../../wailsjs/go/main/HavenApp";
import { ListThreads } from "../../wailsjs/go/main/HavenApp";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { ApprovalInfo, SecurityAlert, WinState } from "../lib/haven";
import { resolveWindowFrame } from "../lib/haven";

export function useHavenMorphSessionEvents(options: {
  activeThreadRef: MutableRefObject<string | null>;
  setMessages: Dispatch<SetStateAction<ConversationEvent[]>>;
  setExecutionState: Dispatch<SetStateAction<string>>;
  setPendingApproval: Dispatch<SetStateAction<ApprovalInfo | null>>;
  setThreads: Dispatch<SetStateAction<ThreadSummary[]>>;
  setSecurityAlerts: Dispatch<SetStateAction<SecurityAlert[]>>;
  setWindows: Dispatch<SetStateAction<Record<string, WinState>>>;
  nextAlertID: MutableRefObject<number>;
  nextZ: MutableRefObject<number>;
}) {
  const {
    activeThreadRef,
    setMessages,
    setExecutionState,
    setPendingApproval,
    setThreads,
    setSecurityAlerts,
    setWindows,
    nextAlertID,
    nextZ,
  } = options;

  useEffect(() => {
    const unsubscribeAssistantMessage = EventsOn("haven:assistant_message", (...args: unknown[]) => {
      const data = args[0] as Record<string, unknown>;
      if ((data.thread_id as string) === activeThreadRef.current) {
        setMessages((previous) => [...previous, {
          v: "",
          ts: new Date().toISOString(),
          thread_id: data.thread_id as string,
          type: "assistant_message",
          data: { text: data.text },
        } as ConversationEvent]);
      }
      ListThreads().then((items) => setThreads(items || []));
    });

    const unsubscribeExecutionState = EventsOn("haven:execution_state", (...args: unknown[]) => {
      const data = args[0] as Record<string, unknown>;
      if ((data.thread_id as string) === activeThreadRef.current) {
        setExecutionState(data.state as string);
      }
    });

    const unsubscribeApprovalRequested = EventsOn("haven:approval_requested", (...args: unknown[]) => {
      const data = args[0] as Record<string, unknown>;
      if ((data.thread_id as string) === activeThreadRef.current) {
        setPendingApproval({
          approvalRequestID: data.approval_request_id as string,
          capability: data.capability as string,
        });
      }
    });

    const unsubscribeToolStarted = EventsOn("haven:tool_started", (...args: unknown[]) => {
      const data = args[0] as Record<string, unknown>;
      if ((data.thread_id as string) === activeThreadRef.current) {
        setMessages((previous) => [...previous, {
          v: "",
          ts: new Date().toISOString(),
          thread_id: data.thread_id as string,
          type: "orchestration.tool_started",
          data: { call_id: data.call_id, capability: data.capability },
        } as ConversationEvent]);
      }
    });

    const unsubscribeToolResult = EventsOn("haven:tool_result", (...args: unknown[]) => {
      const data = args[0] as Record<string, unknown>;
      if ((data.thread_id as string) === activeThreadRef.current) {
        setMessages((previous) => [...previous, {
          v: "",
          ts: new Date().toISOString(),
          thread_id: data.thread_id as string,
          type: "orchestration.tool_result",
          data: { call_id: data.call_id, capability: data.capability, status: data.status, output: data.output },
        } as ConversationEvent]);
      }
    });

    const unsubscribeSecurityAlert = EventsOn("haven:security_alert", (...args: unknown[]) => {
      const data = args[0] as Record<string, unknown>;
      const alert: SecurityAlert = {
        id: nextAlertID.current++,
        type: (data.type as string) || "unknown",
        message: (data.message as string) || "Security event",
        ts: new Date().toISOString(),
      };
      setSecurityAlerts((previous) => [alert, ...previous].slice(0, 50));
      setWindows((previous) => {
        if (previous.loopgate) return previous;
        const frame = resolveWindowFrame("loopgate");
        return {
          ...previous,
          loopgate: { id: "loopgate", x: frame.x, y: frame.y, w: frame.w, h: frame.h, title: frame.title, z: ++nextZ.current, collapsed: false },
        };
      });
    });

    return () => {
      unsubscribeAssistantMessage();
      unsubscribeExecutionState();
      unsubscribeApprovalRequested();
      unsubscribeToolStarted();
      unsubscribeToolResult();
      unsubscribeSecurityAlert();
    };
  }, [
    activeThreadRef,
    setMessages,
    setExecutionState,
    setPendingApproval,
    setThreads,
    setSecurityAlerts,
    setWindows,
    nextAlertID,
    nextZ,
  ]);
}

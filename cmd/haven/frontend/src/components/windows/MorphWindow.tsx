import { useCallback, useEffect, useRef, useState, type Ref } from "react";

import type {
  SystemStatusResponse,
  ConversationEvent,
  MemoryStatusResponse,
  PresenceResponse,
  ThreadSummary,
} from "../../../wailsjs/go/main/HavenApp";
import type { ApprovalInfo } from "../../lib/haven";
import { formatTime } from "../../lib/haven";
import ApprovalDialog from "../ApprovalDialog";
import ChatMessageText from "../ChatMessageText";

type AmbientTurnRole = "user" | "assistant" | "tool";
type AmbientTurnTone = "present" | "settling" | "echo" | "ghost" | "settled";

interface AmbientTurn {
  id: string;
  role: AmbientTurnRole;
  text: string;
  tone: AmbientTurnTone;
  toolCapability?: string;
  toolStatus?: string;
}

export interface MorphWindowProps {
  threads: ThreadSummary[];
  activeThreadID: string | null;
  messages: ConversationEvent[];
  pendingApproval: ApprovalInfo | null;
  input: string;
  isRunning: boolean;
  executionState: string;
  presence: PresenceResponse;
  memoryStatus: MemoryStatusResponse | null;
  systemStatus: SystemStatusResponse | null;
  messagesEndRef: Ref<HTMLDivElement>;
  onCreateThread: () => void;
  onSelectThread: (threadID: string) => void;
  onOpenThreadContextMenu: (x: number, y: number, threadID: string) => void;
  onHandleApproval: (approved: boolean) => void;
  onCancel: () => void;
  onInputChange: (value: string) => void;
  onSend: () => void;
}

function ambientTurnText(rawData: unknown): string {
  if (!rawData || typeof rawData !== "object") return "";
  const parsedMessageData = rawData as Record<string, unknown>;
  return typeof parsedMessageData.text === "string" ? parsedMessageData.text.trim() : "";
}

function toolTurnText(rawData: unknown): { capability: string; status: string; output: string } {
  if (!rawData || typeof rawData !== "object") return { capability: "", status: "", output: "" };
  const data = rawData as Record<string, unknown>;
  return {
    capability: typeof data.capability === "string" ? data.capability : "",
    status: typeof data.status === "string" ? data.status : "",
    output: typeof data.output === "string" ? data.output.trim() : "",
  };
}

type TurnWithoutTone = { id: string; role: AmbientTurnRole; text: string; toolCapability?: string; toolStatus?: string };

function deriveAmbientTurns(messages: ConversationEvent[]): AmbientTurn[] {
  const orderedConversationTurns: TurnWithoutTone[] = [];
  for (let index = 0; index < messages.length; index++) {
    const message = messages[index];
    if (message.type === "orchestration.tool_result") {
      const tool = toolTurnText(message.data);
      if (!tool.capability) continue;
      const preview = tool.output ? tool.output.slice(0, 120) + (tool.output.length > 120 ? "..." : "") : "";
      orderedConversationTurns.push({
        id: `tool-${index}`,
        role: "tool",
        text: preview,
        toolCapability: tool.capability,
        toolStatus: tool.status,
      });
      continue;
    }

    if (message.type !== "user_message" && message.type !== "assistant_message") {
      continue;
    }

    const turnText = ambientTurnText(message.data);
    if (!turnText) continue;

    orderedConversationTurns.push({
      id: `${message.type}-${index}`,
      role: message.type === "user_message" ? "user" : "assistant",
      text: turnText,
    });
  }

  return orderedConversationTurns.map((turn, index) => {
    const turnsFromEnd = orderedConversationTurns.length - 1 - index;
    let tone: AmbientTurnTone = "settled";

    if (turnsFromEnd === 0) {
      tone = "present";
    } else if (turnsFromEnd === 1) {
      tone = "settling";
    } else if (turnsFromEnd === 2) {
      tone = "echo";
    } else if (turnsFromEnd === 3) {
      tone = "ghost";
    }

    return {
      ...turn,
      tone,
    };
  });
}

function activeThreadSummary(activeThreadID: string | null, threads: ThreadSummary[]): ThreadSummary | null {
  if (!activeThreadID) {
    return null;
  }
  return threads.find((thread) => thread.thread_id === activeThreadID) ?? null;
}

export default function MorphWindow({
  threads,
  activeThreadID,
  messages,
  pendingApproval,
  input,
  isRunning,
  executionState,
  presence,
  memoryStatus,
  systemStatus,
  messagesEndRef,
  onCreateThread,
  onSelectThread,
  onOpenThreadContextMenu,
  onHandleApproval,
  onCancel,
  onInputChange,
  onSend,
}: MorphWindowProps) {
  const [recallOpen, setRecallOpen] = useState(false);
  const [memoryOpen, setMemoryOpen] = useState(false);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setRecallOpen(false);
  }, [activeThreadID]);

  // Scroll to bottom whenever thread changes or messages arrive.
  useEffect(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
      setIsAtBottom(true);
    }
  }, [activeThreadID, messages.length]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const threshold = 60;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < threshold;
    setIsAtBottom(atBottom);
  }, []);

  const scrollToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    }
  }, []);

  const currentThread = activeThreadSummary(activeThreadID, threads);
  const visibleTurns = deriveAmbientTurns(messages);
  const activeWorkers = systemStatus?.workers ?? [];
  const unresolvedCount = memoryStatus?.unresolved_item_count ?? 0;
  const rememberedFactCount = memoryStatus?.remembered_fact_count ?? 0;
  const rememberedFacts = memoryStatus?.remembered_facts ?? [];
  const activeGoalCount = memoryStatus?.active_goal_count ?? 0;
  const memorySummary = memoryStatus?.wake_state_summary || "Morph is settling into the room.";
  const memoryFocus = memoryStatus?.current_focus;
  const memoryMeta = `${rememberedFactCount} memories · ${activeGoalCount} goals · ${unresolvedCount} open`;

  return (
    <div className="morph-app morph-room">
      <div className="morph-room-toolbar">
        <div className="morph-room-presence">
          <div
            className={`morph-room-orb state-${presence.state} ${executionState === "running" ? "is-running" : ""}`}
            aria-hidden="true"
          >
            <div className="morph-room-orb-halo" />
            <div className="morph-room-orb-ring" />
            <div className="morph-room-orb-core" />
          </div>
          <div className="morph-room-presence-copy">
            <div className="morph-room-kicker">Morph</div>
            <div className="morph-room-status">{presence.status_text}</div>
            <div className="morph-room-detail">
              {presence.detail_text || memoryStatus?.current_focus || "Here with you in Haven."}
            </div>
          </div>
        </div>

        <div className="morph-room-actions">
          <button
            className={`morph-room-action ${recallOpen ? "active" : ""}`}
            onClick={() => setRecallOpen((open) => !open)}
          >
            Recall
          </button>
          <button className="morph-room-action" onClick={onCreateThread}>
            Start Fresh
          </button>
        </div>
      </div>

      <div className="morph-room-body">
        <section className="morph-room-stage">
          <aside className={`morph-room-sidecar ${memoryOpen ? "open" : ""}`}>
            <button
              className={`morph-room-memory-card ${memoryOpen ? "active" : ""}`}
              onClick={() => setMemoryOpen((open) => !open)}
              aria-expanded={memoryOpen}
            >
              <span className="morph-room-memory-card-kicker">Continuity</span>
              <span className="morph-room-memory-card-summary">{memorySummary}</span>
              {memoryFocus && <span className="morph-room-memory-card-focus">{memoryFocus}</span>}
              <span className="morph-room-memory-card-meta">{memoryMeta}</span>
            </button>

            {memoryOpen && (
              <div className="morph-room-memory-panel">
                <div className="morph-room-memory-panel-header">
                  <div>
                    <div className="morph-room-memory-panel-kicker">Current room state</div>
                    <div className="morph-room-memory-summary">{memorySummary}</div>
                  </div>
                  <button className="morph-room-memory-hide" onClick={() => setMemoryOpen(false)}>
                    Hide
                  </button>
                </div>

                <div className="morph-room-memory-pills">
                  <span className="morph-room-pill">{rememberedFactCount} memories</span>
                  <span className="morph-room-pill">{activeGoalCount} goals</span>
                  <span className="morph-room-pill">{unresolvedCount} open</span>
                </div>

                {memoryFocus && <div className="morph-room-memory-focus">Current focus: {memoryFocus}</div>}

                {rememberedFacts.length > 0 ? (
                  <div className="morph-room-memory-facts">
                    {rememberedFacts.slice(0, 4).map((fact) => (
                      <div key={`${fact.name}-${fact.value}`} className="morph-room-memory-fact">
                        <span className="morph-room-memory-fact-name">{fact.name}</span>
                        <span className="morph-room-memory-fact-value">{fact.value}</span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="morph-room-memory-empty">Nothing stable is pinned here yet.</div>
                )}
              </div>
            )}
          </aside>

          <div className="morph-room-main">
            <div className="morph-room-canvas">
              <div className="morph-room-session-meta">
                <span className="morph-room-session-label">
                  {currentThread?.title || (activeThreadID ? "Open conversation" : "A quiet room")}
                </span>
                {currentThread?.updated_at && (
                  <span className="morph-room-session-time">last touched {formatTime(currentThread.updated_at)}</span>
                )}
              </div>

              <div className="morph-room-scroll" ref={scrollRef} onScroll={handleScroll}>
                {visibleTurns.length > 0 ? (
                  <div className="morph-room-turns">
                    {visibleTurns.map((turn) => (
                      turn.role === "tool" ? (
                        <div key={turn.id} className={`morph-room-tool morph-room-turn-${turn.tone}`}>
                          <span className={`morph-room-tool-dot ${turn.toolStatus === "success" ? "dot-green" : turn.toolStatus === "error" ? "dot-red" : "dot-amber"}`} />
                          <span className="morph-room-tool-name">{turn.toolCapability}</span>
                          {turn.text && <span className="morph-room-tool-output">{turn.text}</span>}
                        </div>
                      ) : (
                        <div key={turn.id} className={`morph-room-turn morph-room-turn-${turn.role} morph-room-turn-${turn.tone}`}>
                          <div className="morph-room-turn-speaker">{turn.role === "assistant" ? "Morph" : "You"}</div>
                          <div className="morph-room-turn-text">
                            {turn.role === "assistant" ? <ChatMessageText text={turn.text} /> : turn.text}
                          </div>
                        </div>
                      )
                    ))}
                    <div ref={messagesEndRef} />
                  </div>
                ) : (
                  <div className="morph-room-empty-state">
                    <div className="morph-room-empty-title">
                      {activeThreadID ? "The room is quiet for a moment." : "Morph is ready when you are."}
                    </div>
                    <div className="morph-room-empty-copy">
                      {activeThreadID
                        ? "Speak naturally. The conversation will linger for a moment and then recede into memory."
                        : "Start a fresh conversation or reopen one from Recall. The full history stays durable underneath, but this room stays uncluttered."}
                    </div>
                  </div>
                )}

                {executionState === "running" && (
                  <div className="morph-room-thinking">
                    <span className="dot dot-amber dot-pulse" />
                    <span>Morph is responding...</span>
                    <button className="morph-room-cancel" onClick={onCancel}>
                      Stop
                    </button>
                  </div>
                )}

                {activeWorkers.length > 0 && (
                  <div className="morph-room-workers">
                    {activeWorkers.map((worker) => (
                      <div key={worker.id} className="morph-room-worker">
                        <span className={`dot ${worker.state === "active" ? "dot-amber dot-pulse" : "dot-green"}`} />
                        <span className="morph-room-worker-id">{worker.id}</span>
                        <span className="morph-room-worker-goal">{worker.goal || worker.state}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {!isAtBottom && visibleTurns.length > 0 && (
                <button className="morph-room-scroll-bottom" onClick={scrollToBottom} title="Jump to latest">
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M8 3v10M4 9l4 4 4-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>
                </button>
              )}
            </div>
          </div>
        </section>

        <aside className={`morph-room-recall ${recallOpen ? "open" : ""}`}>
          <div className="morph-room-recall-header">
            <div className="morph-room-recall-title">Recall</div>
            <div className="morph-room-recall-copy">Durable conversation history lives here when you need it.</div>
          </div>

          <div className="morph-room-recall-list">
            {threads.length > 0 ? (
              threads.map((thread) => (
                <button
                  key={thread.thread_id}
                  className={`morph-room-recall-item ${thread.thread_id === activeThreadID ? "active" : ""}`}
                  onClick={() => onSelectThread(thread.thread_id)}
                  onContextMenu={(event) => {
                    event.preventDefault();
                    onOpenThreadContextMenu(event.clientX, event.clientY, thread.thread_id);
                  }}
                >
                  <span className="morph-room-recall-item-title">{thread.title || "Untitled conversation"}</span>
                  <span className="morph-room-recall-item-time">{formatTime(thread.updated_at)}</span>
                </button>
              ))
            ) : (
              <div className="morph-room-recall-empty">No prior conversations yet.</div>
            )}
          </div>
        </aside>
      </div>

      <div className="morph-room-input-dock">
        <textarea
          className="morph-room-input"
          value={input}
          onChange={(event) => onInputChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              onSend();
            }
          }}
          placeholder="Write to Morph..."
          disabled={isRunning}
          rows={1}
          onInput={(event) => {
            const target = event.currentTarget;
            target.style.height = "auto";
            target.style.height = `${Math.min(target.scrollHeight, 120)}px`;
          }}
        />
        <button className="retro-btn primary morph-room-send" onClick={onSend} disabled={isRunning || !input.trim()}>
          Reply
        </button>
      </div>

      {pendingApproval && (
        <ApprovalDialog
          capability={pendingApproval.capability}
          onAllow={() => onHandleApproval(true)}
          onDeny={() => onHandleApproval(false)}
        />
      )}
    </div>
  );
}

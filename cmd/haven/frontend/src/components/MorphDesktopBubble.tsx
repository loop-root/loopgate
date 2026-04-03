import { useCallback, useEffect, useRef, useState } from "react";

import type { ConversationEvent } from "../../wailsjs/go/main/HavenApp";
import type { ApprovalInfo } from "../lib/haven";
import { MorphGlyph } from "../lib/haven";
import ApprovalDialog from "./ApprovalDialog";
import ChatMessageText from "./ChatMessageText";

export interface MorphDesktopBubbleProps {
  presenceState: string;
  presenceStatusText: string;
  messages: ConversationEvent[];
  pendingApproval: ApprovalInfo | null;
  input: string;
  isRunning: boolean;
  executionState: string;
  onHandleApproval: (approved: boolean) => void;
  onCancel: () => void;
  onInputChange: (value: string) => void;
  onSend: () => void;
}

interface BubbleTurn {
  id: string;
  role: "user" | "assistant";
  text: string;
}

function deriveBubbleTurns(messages: ConversationEvent[]): BubbleTurn[] {
  const turns: BubbleTurn[] = [];
  for (let index = 0; index < messages.length; index++) {
    const message = messages[index];
    if (message.type !== "user_message" && message.type !== "assistant_message") continue;
    if (!message.data || typeof message.data !== "object") continue;
    const parsedData = message.data as Record<string, unknown>;
    const text = typeof parsedData.text === "string" ? parsedData.text.trim() : "";
    if (!text) continue;
    turns.push({
      id: `${message.type}-${index}`,
      role: message.type === "user_message" ? "user" : "assistant",
      text,
    });
  }
  return turns;
}

export default function MorphDesktopBubble({
  presenceState,
  presenceStatusText,
  messages,
  pendingApproval,
  input,
  isRunning,
  executionState,
  onHandleApproval,
  onCancel,
  onInputChange,
  onSend,
}: MorphDesktopBubbleProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [isAtBottom, setIsAtBottom] = useState(true);

  const turns = deriveBubbleTurns(messages);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
      setIsAtBottom(true);
    }
  }, [messages.length]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    setIsAtBottom(el.scrollHeight - el.scrollTop - el.clientHeight < 40);
  }, []);

  const scrollToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
  }, []);

  return (
    <div className="morph-desktop-bubble" onClick={(event) => event.stopPropagation()}>
      <div className="morph-bubble-scroll" ref={scrollRef} onScroll={handleScroll}>
        {turns.length > 0 ? (
          <div className="morph-bubble-turns">
            {turns.map((turn) => (
              <div key={turn.id} className={`morph-bubble-msg morph-bubble-msg--${turn.role}`}>
                <div className="morph-bubble-msg-speaker">{turn.role === "assistant" ? "Morph" : "You"}</div>
                <div className="morph-bubble-msg-text">
                  {turn.role === "assistant" ? <ChatMessageText text={turn.text} /> : turn.text}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="morph-bubble-empty">Haven is quiet.</div>
        )}

        {executionState === "running" && (
          <div className="morph-bubble-thinking">
            <span className="dot dot-amber dot-pulse" />
            <span>Morph is thinking...</span>
            <button className="morph-bubble-cancel" type="button" onClick={onCancel}>Stop</button>
          </div>
        )}
      </div>

      {!isAtBottom && turns.length > 0 && (
        <button className="morph-bubble-scroll-btn" type="button" onClick={scrollToBottom} title="Jump to latest">
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none"><path d="M8 3v10M4 9l4 4 4-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>
        </button>
      )}

      <div className="morph-bubble-composer">
        <textarea
          className="morph-bubble-input"
          value={input}
          onChange={(event) => onInputChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); onSend(); }
          }}
          placeholder="Write to Morph..."
          disabled={isRunning}
          rows={1}
          onInput={(event) => {
            const target = event.currentTarget;
            target.style.height = "auto";
            target.style.height = `${Math.min(target.scrollHeight, 80)}px`;
          }}
        />
        <button className="morph-bubble-send" type="button" onClick={onSend} disabled={isRunning || !input.trim()}>Reply</button>
      </div>

      <div className="morph-bubble-avatar">
        <MorphGlyph size={48} className="morph-bubble-glyph" />
        <span className={`morph-bubble-status-dot morph-bubble-status-dot--${presenceState}`} />
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

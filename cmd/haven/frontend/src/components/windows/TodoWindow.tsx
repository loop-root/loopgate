import { useMemo, useState } from "react";

import type { MemoryStatusResponse, StandingTaskGrantSummary, TaskDraft } from "../../../wailsjs/go/main/HavenApp";

export interface TodoWindowProps {
  memoryStatus: MemoryStatusResponse | null;
  onAddTask: (request: TaskDraft) => Promise<string | null>;
  onCompleteTodo: (itemID: string) => Promise<string | null>;
  standingTaskGrants: StandingTaskGrantSummary[];
}

function taskKindLabel(taskKind?: string): string {
  switch (taskKind) {
    case "one_off":
      return "one-off";
    case "scheduled":
      return "scheduled";
    case "carry_over":
      return "carry-over";
    default:
      return "task";
  }
}

function sourceKindLabel(sourceKind?: string): string {
  switch (sourceKind) {
    case "folder_signal":
      return "folder signal";
    case "startup_offer":
      return "startup offer";
    case "system":
      return "system";
    case "continuity":
      return "continuity";
    case "user":
      return "user";
    default:
      return sourceKind ? sourceKind.split("_").join(" ") : "manual";
  }
}

function formatScheduledAt(rawTimestamp?: string): string {
  if (!rawTimestamp) {
    return "";
  }
  try {
    return new Date(rawTimestamp).toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    });
  } catch {
    return rawTimestamp;
  }
}

function isScheduledTaskDue(rawTimestamp?: string): boolean {
  if (!rawTimestamp) {
    return false;
  }
  return new Date(rawTimestamp).getTime() <= Date.now();
}

function taskNeedsApproval(executionClass: string | undefined, standingGrantByClass: Map<string, StandingTaskGrantSummary>): boolean {
  if (!executionClass || executionClass === "approval_required") {
    return true;
  }
  return !standingGrantByClass.get(executionClass)?.granted;
}

export default function TodoWindow({ memoryStatus, onAddTask, onCompleteTodo, standingTaskGrants }: TodoWindowProps) {
  const unresolvedItems = memoryStatus?.unresolved_items || [];
  const activeGoals = memoryStatus?.active_goals || [];
  const scheduledItems = unresolvedItems.filter((item) => item.task_kind === "scheduled");
  const dueScheduledCount = useMemo(() => scheduledItems.filter((item) => isScheduledTaskDue(item.scheduled_for_utc)).length, [scheduledItems]);
  const standingGrantByClass = useMemo(() => {
    return new Map(standingTaskGrants.map((grant) => [grant.class, grant]));
  }, [standingTaskGrants]);
  const [draftText, setDraftText] = useState("");
  const [draftNextStep, setDraftNextStep] = useState("");
  const [draftScheduledForLocal, setDraftScheduledForLocal] = useState("");
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [errorText, setErrorText] = useState("");

  const submitTask = async () => {
    const trimmedText = draftText.trim();
    if (!trimmedText) {
      setErrorText("Write a short task first.");
      return;
    }
    const trimmedNextStep = draftNextStep.trim();
    let scheduledForUTC = "";
    if (draftScheduledForLocal.trim()) {
      const localDate = new Date(draftScheduledForLocal);
      if (Number.isNaN(localDate.getTime())) {
        setErrorText("Pick a valid scheduled time.");
        return;
      }
      scheduledForUTC = localDate.toISOString();
    }
    setBusyAction("add");
    setErrorText("");
    try {
      const actionError = await onAddTask({
        text: trimmedText,
        next_step: trimmedNextStep || undefined,
        scheduled_for_utc: scheduledForUTC || undefined,
      });
      if (actionError) {
        setErrorText(actionError);
        return;
      }
      setDraftText("");
      setDraftNextStep("");
      setDraftScheduledForLocal("");
    } finally {
      setBusyAction(null);
    }
  };

  const completeTodo = async (itemID: string) => {
    setBusyAction(itemID);
    setErrorText("");
    try {
      const actionError = await onCompleteTodo(itemID);
      if (actionError) {
        setErrorText(actionError);
      }
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="todo-app">
      <div className="app-hero todo-hero todo-header">
        <div className="app-kicker">Task Board</div>
        <div className="app-title">What Morph is carrying, planning, and preparing across sessions.</div>
        <div className="app-subtitle">
          This is operational memory. Open work stays visible here so Haven can reopen like an active computer, not a blank slate.
        </div>
        <div className="app-summary-pills">
          <span className="app-summary-pill">{unresolvedItems.length} open</span>
          <span className="app-summary-pill">{scheduledItems.length} scheduled</span>
          <span className="app-summary-pill">{dueScheduledCount} due</span>
          <span className="app-summary-pill">{activeGoals.length} goals</span>
        </div>
      </div>

      <div className="todo-compose">
        <input
          className="todo-input"
          value={draftText}
          onChange={(event) => setDraftText(event.target.value)}
          placeholder="Add something Morph should carry forward..."
          maxLength={200}
          disabled={busyAction === "add"}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              void submitTask();
            }
          }}
        />
        <button
          className="todo-add-btn"
          onClick={() => { void submitTask(); }}
          disabled={busyAction === "add"}
        >
          {busyAction === "add" ? "Adding..." : "Add"}
        </button>
      </div>
      <div className="todo-compose-secondary">
        <input
          className="todo-input todo-input-secondary"
          value={draftNextStep}
          onChange={(event) => setDraftNextStep(event.target.value)}
          placeholder="Optional next step"
          maxLength={200}
          disabled={busyAction === "add"}
        />
        <input
          className="todo-input todo-input-secondary"
          type="datetime-local"
          value={draftScheduledForLocal}
          onChange={(event) => setDraftScheduledForLocal(event.target.value)}
          disabled={busyAction === "add"}
        />
      </div>
      {errorText && <div className="todo-error">{errorText}</div>}

      <div className="todo-body">
        <section className="todo-section">
          <div className="todo-section-title">Open Tasks</div>
          {unresolvedItems.length === 0 ? (
            <div className="todo-empty">Nothing is waiting right now.</div>
          ) : (
            <div className="todo-list">
              {unresolvedItems.map((item) => {
                const scheduledAtLabel = formatScheduledAt(item.scheduled_for_utc);
                const executionGrant = item.execution_class ? standingGrantByClass.get(item.execution_class) : undefined;
                const requiresApproval = taskNeedsApproval(item.execution_class, standingGrantByClass);
                return (
                  <div key={item.id} className="todo-item todo-item-actionable">
                    <span className="todo-item-dot todo-item-dot-open" />
                    <div className="todo-item-copy">
                      <div className="todo-item-text">{item.text || item.id}</div>
                      <div className="todo-item-tags">
                        <span className="todo-item-tag">{taskKindLabel(item.task_kind)}</span>
                        <span className="todo-item-tag">{sourceKindLabel(item.source_kind)}</span>
                        {executionGrant && <span className="todo-item-tag">{executionGrant.label}</span>}
                        {scheduledAtLabel && <span className="todo-item-tag">{scheduledAtLabel}</span>}
                        {item.scheduled_for_utc && isScheduledTaskDue(item.scheduled_for_utc) && (
                          <span className="todo-item-tag todo-item-tag-due">due now</span>
                        )}
                        <span className={`todo-item-tag ${requiresApproval ? "todo-item-tag-approval" : "todo-item-tag-granted"}`}>
                          {requiresApproval ? "asks first" : "always allowed in Haven"}
                        </span>
                      </div>
                      {item.next_step && <div className="todo-item-next">Next: {item.next_step}</div>}
                      {item.id && <div className="todo-item-meta">{item.id}</div>}
                    </div>
                    <button
                      className="todo-complete-btn"
                      onClick={() => { void completeTodo(item.id); }}
                      disabled={busyAction === item.id}
                    >
                      {busyAction === item.id ? "Saving..." : "Done"}
                    </button>
                  </div>
                );
              })}
            </div>
          )}
        </section>

        <section className="todo-section">
          <div className="todo-section-title">Active Goals</div>
          {activeGoals.length === 0 ? (
            <div className="todo-empty">No active goals are pinned right now.</div>
          ) : (
            <div className="todo-list">
              {activeGoals.map((goal, index) => (
                <div key={`${goal}-${index}`} className="todo-item">
                  <span className="todo-item-dot todo-item-dot-goal" />
                  <div className="todo-item-copy">
                    <div className="todo-item-text">{goal}</div>
                    <div className="todo-item-meta">held in continuity</div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>
    </div>
  );
}

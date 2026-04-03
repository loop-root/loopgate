import type { AppID } from "../lib/haven";

export interface WorkspaceEditState {
  path: string;
  content: string;
  dirty: boolean;
}

export interface WorkspaceNewNameState {
  type: "file" | "dir";
}

export interface PendingIconDragState {
  id: AppID;
  startX: number;
  startY: number;
  rectX: number;
  rectY: number;
}

export interface PendingFileDragState {
  id: string;
  startX: number;
  startY: number;
}

export interface ThreadRenameDialogState {
  threadID: string;
  value: string;
}

export type ConfirmDialogState =
  | { kind: "discard_editor_changes"; path: string }
  | { kind: "delete_workspace_entry"; path: string; entryName: string }
  | { kind: "discard_review_changes"; path: string };

export const DRAG_THRESHOLD = 5;
export const NEW_WORKING_NOTE_PATH = "__new_note__";

export function finiteWindowValue(rawValue: unknown): number | undefined {
  return typeof rawValue === "number" && Number.isFinite(rawValue) ? rawValue : undefined;
}

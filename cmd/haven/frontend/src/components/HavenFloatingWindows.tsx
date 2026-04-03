import type { ComponentProps } from "react";

import HavenWindow from "./HavenWindow";
import JournalWindow from "./windows/JournalWindow";
import LoopgateWindow from "./windows/LoopgateWindow";
import MorphWindow from "./windows/MorphWindow";
import NotesWindow from "./windows/NotesWindow";
import PaintWindow from "./windows/PaintWindow";
import SettingsPanel from "./SettingsPanel";
import TodoWindow from "./windows/TodoWindow";
import WorkspaceWindow from "./windows/WorkspaceWindow";
import type { WinState } from "../lib/haven";

export interface HavenFloatingWindowsProps {
  openWinList: WinState[];
  /** When set, these window ids are not rendered as floaters (e.g. docked in workstation layout). */
  excludeWindowIds?: readonly string[];
  focusedWin: string | null;
  onFocusWindow: (id: string) => void;
  onCloseWindow: (id: string) => void;
  onCollapseWindow: (id: string) => void;
  onDragWindow: (id: string, x: number, y: number) => void;
  onResizeWindow: (id: string, x: number, y: number, w: number, h: number) => void;
  morphProps: ComponentProps<typeof MorphWindow>;
  loopgateProps: ComponentProps<typeof LoopgateWindow>;
  workspaceProps: ComponentProps<typeof WorkspaceWindow>;
  settingsProps: ComponentProps<typeof SettingsPanel>;
  todoProps: ComponentProps<typeof TodoWindow>;
  notesProps: ComponentProps<typeof NotesWindow>;
  journalProps: ComponentProps<typeof JournalWindow>;
  paintOnToast: ComponentProps<typeof PaintWindow>["onToast"];
}

export default function HavenFloatingWindows({
  openWinList,
  excludeWindowIds,
  focusedWin,
  onFocusWindow,
  onCloseWindow,
  onCollapseWindow,
  onDragWindow,
  onResizeWindow,
  morphProps,
  loopgateProps,
  workspaceProps,
  settingsProps,
  todoProps,
  notesProps,
  journalProps,
  paintOnToast,
}: HavenFloatingWindowsProps) {
  const docked = excludeWindowIds ? new Set(excludeWindowIds) : null;
  const visibleWinList = docked ? openWinList.filter((win) => !docked.has(win.id)) : openWinList;

  return (
    <div className="haven-floating-windows-root">
      {visibleWinList.map((win) => (
        <HavenWindow
          key={win.id}
          win={win}
          focused={focusedWin === win.id}
          onFocus={() => onFocusWindow(win.id)}
          onClose={() => onCloseWindow(win.id)}
          onCollapse={() => onCollapseWindow(win.id)}
          onDrag={(x, y) => onDragWindow(win.id, x, y)}
          onResize={(x, y, w, h) => onResizeWindow(win.id, x, y, w, h)}
        >
          {win.id === "morph" && <MorphWindow {...morphProps} />}
          {win.id === "loopgate" && <LoopgateWindow {...loopgateProps} />}
          {win.id === "workspace" && <WorkspaceWindow {...workspaceProps} />}
          {win.id === "settings" && <SettingsPanel {...settingsProps} />}
          {win.id === "todo" && <TodoWindow {...todoProps} />}
          {win.id === "notes" && <NotesWindow {...notesProps} />}
          {win.id === "journal" && <JournalWindow {...journalProps} />}
          {win.id === "paint" && <PaintWindow onToast={paintOnToast} />}
        </HavenWindow>
      ))}
    </div>
  );
}

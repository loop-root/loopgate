import type { SystemStatusResponse } from "../../wailsjs/go/main/HavenApp";
import type { HavenDockEdge } from "../hooks/useHavenDockEdge";
import type { AppID, WinState } from "../lib/haven";
import { ICON_MAP, WIN_DEFAULTS, dotClass, taskbarStatusLabel } from "../lib/haven";

export interface TaskBarProps {
  dockEdge: HavenDockEdge;
  launcherAppIDs: AppID[];
  windows: Record<string, WinState>;
  focusedWin: string | null;
  executionState: string;
  pendingApprovalCount: number;
  continuityCount: number;
  systemStatus: SystemStatusResponse | null;
  clock: string;
  onDockLaunch: (id: AppID) => void;
}

export default function TaskBar({
  dockEdge,
  launcherAppIDs,
  windows,
  focusedWin,
  executionState,
  pendingApprovalCount,
  continuityCount,
  systemStatus,
  clock,
  onDockLaunch,
}: TaskBarProps) {
  const edgeClass = `haven-taskbar--edge-${dockEdge}`;

  return (
    <div className={`taskbar ${edgeClass}`} role="toolbar" aria-label="Haven dock and status">
      <div className="taskbar-spacer" style={{ flex: 1 }} aria-hidden />
      <div className="haven-dock-launchers">
        {launcherAppIDs.map((appID) => {
          const Icon = ICON_MAP[appID];
          const win = windows[appID];
          const isRunning = Boolean(win) && !win.collapsed;
          const isFocused = focusedWin === appID;
          const active = isRunning;
          const showLoopgateBadge = appID === "loopgate" && pendingApprovalCount > 0;
          const showTodoBadge = appID === "todo" && continuityCount > 0;

          return (
            <button
              key={appID}
              type="button"
              className={`haven-dock-tile ${active ? "haven-dock-tile--active" : ""} ${isFocused ? "haven-dock-tile--focused" : ""}`}
              title={WIN_DEFAULTS[appID].title}
              onClick={() => onDockLaunch(appID)}
            >
              <span className="haven-dock-tile-plate" aria-hidden />
              <span className="haven-dock-tile-inner">
                {Icon ? <Icon /> : null}
                {showLoopgateBadge && <span className="haven-dock-badge">{pendingApprovalCount}</span>}
                {showTodoBadge && <span className="haven-dock-badge">{continuityCount}</span>}
              </span>
              <span className="haven-dock-tile-label">{WIN_DEFAULTS[appID].title}</span>
            </button>
          );
        })}
      </div>
      <div className="taskbar-sep" aria-hidden />
      <div className="taskbar-tray" style={{ flex: 1, justifyContent: "flex-end" }}>
        <span
          className="taskbar-tray-pill"
          title="Morph run and Loopgate approvals: amber while working, green when idle with nothing waiting, red if the last run stopped with an error."
        >
          <span className={dotClass(executionState)} aria-hidden />
          <span>{taskbarStatusLabel(executionState, pendingApprovalCount)}</span>
        </span>
        <span className="taskbar-tray-pill">Workers {systemStatus?.active_workers ?? 0}/{systemStatus?.max_workers ?? 5}</span>
        <span className="taskbar-tray-clock">{clock}</span>
      </div>
    </div>
  );
}

import type { SystemStatusResponse, MemoryStatusResponse, PresenceResponse } from "../../wailsjs/go/main/HavenApp";
import type { HavenDockEdge } from "../hooks/useHavenDockEdge";
import type { HavenShellLayout } from "../hooks/useHavenShellLayout";
import { HavenWordmark, morphLabel } from "../lib/haven";

interface MenuBarProps {
  openMenu: string | null;
  executionState: string;
  presence: PresenceResponse;
  systemStatus: SystemStatusResponse | null;
  memoryStatus: MemoryStatusResponse | null;
  clock: string;
  shellLayout?: HavenShellLayout;
  dockEdge?: HavenDockEdge;
  onToggleMenu: (label: string) => void;
  onAction: (action: string) => void;
}

function buildMenus(shellLayout: HavenShellLayout | undefined, dockEdge: HavenDockEdge | undefined) {
  const viewItems: (
    | { label: string; id: string; disabled?: boolean }
    | { sep: true }
  )[] = [];
  if (shellLayout) {
    viewItems.push(
      { label: "Workstation layout", id: "layout-workstation", disabled: shellLayout === "workstation" },
      { label: "Classic desktop layout", id: "layout-classic", disabled: shellLayout === "classic" },
      { sep: true },
    );
  }
  if (dockEdge) {
    viewItems.push(
      { label: "Dock on bottom", id: "dock-edge-bottom", disabled: dockEdge === "bottom" },
      { label: "Dock on left", id: "dock-edge-left", disabled: dockEdge === "left" },
      { label: "Dock on right", id: "dock-edge-right", disabled: dockEdge === "right" },
      { sep: true },
    );
  }
  viewItems.push(
    { label: "Arrange Windows", id: "arrange" },
    { sep: true },
    { label: "Settings...", id: "open-settings" },
  );

  return [
    {
      label: "File",
      items: [
        { label: "New Thread", id: "new-thread" },
        { sep: true },
        { label: "Import File...", id: "import-file" },
        { label: "Import Folder...", id: "import-folder" },
      ],
    },
    {
      label: "Edit",
      items: [
        { label: "Undo", id: "undo", disabled: true },
        { label: "Cut", id: "cut", disabled: true },
        { label: "Copy", id: "copy", disabled: true },
        { label: "Paste", id: "paste", disabled: true },
      ],
    },
    {
      label: "View",
      items: viewItems,
    },
    {
      label: "Window",
      items: [
        { label: "Morph", id: "open-morph" },
        { label: "Loopgate", id: "open-loopgate" },
        { label: "Workspace", id: "open-workspace" },
        { label: "Tasks", id: "open-todo" },
        { label: "Notes", id: "open-notes" },
        { label: "Journal", id: "open-journal" },
        { label: "Paint", id: "open-paint" },
      ],
    },
    {
      label: "System",
      items: [
        { label: `Morph: ${morphLabel("idle")}`, id: "status", disabled: true },
        { sep: true },
        { label: "Settings...", id: "open-settings" },
      ],
    },
  ] as const;
}

export default function MenuBar({
  openMenu,
  executionState,
  presence,
  systemStatus,
  memoryStatus,
  clock,
  shellLayout,
  dockEdge,
  onToggleMenu,
  onAction,
}: MenuBarProps) {
  const rememberedFactCount = memoryStatus?.remembered_fact_count ?? 0;
  const activeGoalCount = memoryStatus?.active_goal_count ?? 0;
  const unresolvedCount = memoryStatus?.unresolved_item_count ?? 0;
  const memorySummary = memoryStatus?.wake_state_summary || "No continuity data.";
  const memoryFocus = memoryStatus?.current_focus;

  const menus = buildMenus(shellLayout, dockEdge).map((menu) => {
    if (menu.label !== "System") return menu;
    const continuityItems: ({ label: string; id: string; disabled?: boolean } | { sep: true })[] = [
      { label: `Morph: ${morphLabel(executionState)}`, id: "status", disabled: true },
      { sep: true },
      { label: `Workers: ${systemStatus?.active_workers ?? 0}/${systemStatus?.max_workers ?? 5}`, id: "workers", disabled: true },
      { sep: true },
      { label: `Continuity: ${memorySummary.slice(0, 48)}${memorySummary.length > 48 ? "..." : ""}`, id: "continuity-summary", disabled: true },
    ];
    if (memoryFocus) {
      continuityItems.push({ label: `Focus: ${memoryFocus.slice(0, 40)}${memoryFocus.length > 40 ? "..." : ""}`, id: "continuity-focus", disabled: true });
    }
    continuityItems.push(
      { label: `${rememberedFactCount} memories · ${activeGoalCount} goals · ${unresolvedCount} open`, id: "continuity-meta", disabled: true },
      { sep: true },
      { label: "Settings...", id: "open-settings" },
    );
    return { ...menu, items: continuityItems };
  });

  return (
    <div className="menubar">
      <div className="menubar-logo">
        <HavenWordmark compact />
      </div>
      {menus.map((menu) => (
        <div key={menu.label} className={`menubar-item ${openMenu === menu.label ? "open" : ""}`} onClick={(event) => { event.stopPropagation(); onToggleMenu(menu.label); }}>
          {menu.label}
          {openMenu === menu.label && (
            <div className="menu-dropdown" onClick={(event) => event.stopPropagation()}>
              {menu.items.map((item, index) => (
                "sep" in item ? (
                  <div key={index} className="menu-sep" />
                ) : (
                  <div key={index} className={`menu-option ${"disabled" in item && item.disabled ? "disabled" : ""}`} onClick={() => !("disabled" in item && item.disabled) && onAction(item.id)}>
                    {item.label}
                  </div>
                )
              ))}
            </div>
          )}
        </div>
      ))}
      <div className="menubar-spacer" />
      <div className="menubar-status">
        <span className={`dot ${
          presence.state === "working" || presence.state === "thinking" ? "dot-amber dot-pulse" :
          presence.state === "excited" ? "dot-green" :
          presence.state === "sleeping" ? "dot-idle" :
          "dot-green"
        }`} />
        <span>{presence.status_text}</span>
        {presence.detail_text && <span className="menubar-status-detail">{presence.detail_text}</span>}
      </div>
      <span className="menubar-clock">{clock}</span>
    </div>
  );
}

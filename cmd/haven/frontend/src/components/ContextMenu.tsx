import type { ThreadSummary } from "../../wailsjs/go/main/HavenApp";
import type { ContextMenuState } from "../lib/haven";

interface ContextMenuProps {
  contextMenu: ContextMenuState | null;
  threads: ThreadSummary[];
  onClose: () => void;
  onRequestRenameThread: (threadID: string, currentTitle: string) => void;
  onDesktopAction: (action: string) => void;
  onOpenIcon: (iconID: string) => void;
  onOpenDesktopFile: (fileID: string) => void;
  onRemoveDesktopFile: (fileID: string) => void;
  onExportDesktopFile: (fileID: string) => void;
}

export default function ContextMenu({
  contextMenu,
  threads,
  onClose,
  onRequestRenameThread,
  onDesktopAction,
  onOpenIcon,
  onOpenDesktopFile,
  onRemoveDesktopFile,
  onExportDesktopFile,
}: ContextMenuProps) {
  if (!contextMenu) return null;

  const menuData = contextMenu.data;
  const activeThread = menuData.kind === "thread"
    ? threads.find((entry) => entry.thread_id === menuData.threadID)
    : null;

  const renderMenuItem = (label: string, detail: string | null, onClick: () => void, destructive = false) => (
    <button className={`ctx-menu-item ${destructive ? "destructive" : ""}`} onClick={onClick}>
      <span className="ctx-menu-item-label">{label}</span>
      {detail && <span className="ctx-menu-item-detail">{detail}</span>}
    </button>
  );

  return (
    <div className="ctx-menu" style={{ left: contextMenu.x, top: contextMenu.y }} onClick={(event) => event.stopPropagation()}>
      {menuData.kind === "thread" && (
        <>
          <div className="ctx-menu-title">{activeThread?.title || "Conversation"}</div>
          {renderMenuItem("Rename Conversation", "Give this recall entry a clearer name.", () => {
            onRequestRenameThread(menuData.threadID, activeThread?.title || "");
            onClose();
          })}
        </>
      )}

      {menuData.kind === "desktop" && (
        <>
          <div className="ctx-menu-title">Desktop</div>
          {renderMenuItem("New Conversation", "Open a fresh room with Morph.", () => { onDesktopAction("new-thread"); onClose(); })}
          <div className="ctx-menu-sep" />
          {renderMenuItem("Import File...", "Bring a file into Haven's workspace.", () => { onDesktopAction("import-file"); onClose(); })}
          {renderMenuItem("Import Folder...", "Bring a whole folder into Haven.", () => { onDesktopAction("import-folder"); onClose(); })}
          <div className="ctx-menu-sep" />
          {renderMenuItem("Arrange Windows", "Tidy the room around the active work.", () => { onDesktopAction("arrange"); onClose(); })}
          {renderMenuItem("Settings...", "Adjust Haven, Morph, and shared access.", () => { onDesktopAction("open-settings"); onClose(); })}
        </>
      )}

      {menuData.kind === "icon" && (
        <>
          <div className="ctx-menu-title">App</div>
          {renderMenuItem("Open", "Bring this room to the front.", () => { onOpenIcon(menuData.iconID); onClose(); })}
        </>
      )}

      {menuData.kind === "file" && (
        <>
          <div className="ctx-menu-title">Desktop File</div>
          {renderMenuItem("Open in Workspace", "See the file in Haven's file room.", () => { onOpenDesktopFile(menuData.fileID); onClose(); })}
          <div className="ctx-menu-sep" />
          {renderMenuItem("Export to Computer...", "Copy this Haven file back out to your Mac.", () => { onExportDesktopFile(menuData.fileID); onClose(); })}
          {renderMenuItem("Remove from Desktop", "Hide the icon without deleting the file.", () => { onRemoveDesktopFile(menuData.fileID); onClose(); }, true)}
        </>
      )}
    </div>
  );
}

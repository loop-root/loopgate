import type { MouseEvent as ReactMouseEvent, ReactNode } from "react";

import type { DeskNote } from "../../wailsjs/go/main/HavenApp";
import type { DesktopFile, Wallpaper } from "../lib/haven";
import { formatTime, wallpaperBackgroundImage } from "../lib/haven";

export interface HavenWorkstationStageProps {
  wallpaper: Wallpaper;
  selectedIcon: string | null;
  deskNotes: DeskNote[];
  deskNotePositions: Record<string, { x: number; y: number }>;
  desktopFiles: DesktopFile[];
  executingDeskNoteIDs: Record<string, boolean>;
  onDesktopClick: () => void;
  onDesktopContextMenu: (x: number, y: number) => void;
  onSelectDesktopFile: (id: string) => void;
  onOpenDesktopFile: (fileID: string) => void;
  onOpenDesktopFileContextMenu: (x: number, y: number, fileID: string) => void;
  onStartDesktopFileDrag: (event: ReactMouseEvent<HTMLDivElement>, file: DesktopFile) => void;
  onStartDeskNoteDrag: (event: ReactMouseEvent<HTMLDivElement>, noteID: string) => void;
  onExecuteDeskNote: (noteID: string) => void;
  onDismissDeskNote: (noteID: string) => void;
  morphBubble: ReactNode;
  floatingLayer: ReactNode;
}

export default function HavenWorkstationStage({
  wallpaper,
  selectedIcon,
  deskNotes,
  deskNotePositions,
  desktopFiles,
  executingDeskNoteIDs,
  onDesktopClick,
  onDesktopContextMenu,
  onSelectDesktopFile,
  onOpenDesktopFile,
  onOpenDesktopFileContextMenu,
  onStartDesktopFileDrag,
  onStartDeskNoteDrag,
  onExecuteDeskNote,
  onDismissDeskNote,
  morphBubble,
  floatingLayer,
}: HavenWorkstationStageProps) {
  return (
    <div
      className="workstation-stage"
      onClick={onDesktopClick}
      onContextMenu={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onDesktopContextMenu(event.clientX, event.clientY);
      }}
      style={{ backgroundImage: wallpaperBackgroundImage(wallpaper) }}
    >
      {/* Desk notes and desktop files layer */}
      <div className="workstation-env" onClick={(event) => event.stopPropagation()}>
        {deskNotes.map((deskNote, index) => {
          const position = deskNotePositions[deskNote.id] || { x: 48 + index * 20, y: 48 + index * 20 };
          return (
            <div
              key={deskNote.id}
              className={`desk-note desk-note-${deskNote.kind} workstation-desk-note`}
              style={{
                left: position.x,
                top: position.y,
                zIndex: 11 + deskNotes.length - index,
                transform: `rotate(${index % 2 === 0 ? "-1deg" : "0.8deg"})`,
              }}
              onClick={(event) => event.stopPropagation()}
              onMouseDown={(event) => {
                if (event.button !== 0) return;
                event.stopPropagation();
                onStartDeskNoteDrag(event, deskNote.id);
              }}
            >
              <div className="desk-note-pin" />
              <button className="desk-note-dismiss" title="Dismiss note" type="button" onClick={(event) => { event.stopPropagation(); onDismissDeskNote(deskNote.id); }}>&times;</button>
              <div className="desk-note-title">{deskNote.title}</div>
              <div className="desk-note-body">{deskNote.body}</div>
              {deskNote.action && (
                deskNote.action_executed_at_utc ? (
                  <div className="desk-note-executed">Working in Morph</div>
                ) : (
                  <div className="desk-note-actions">
                    <button className="desk-note-action desk-note-action-primary" type="button" disabled={!!executingDeskNoteIDs[deskNote.id]} onClick={(event) => { event.stopPropagation(); onExecuteDeskNote(deskNote.id); }}>
                      {executingDeskNoteIDs[deskNote.id] ? "Starting..." : (deskNote.action.label || "Yes")}
                    </button>
                    <button className="desk-note-action desk-note-action-secondary" type="button" onClick={(event) => { event.stopPropagation(); onDismissDeskNote(deskNote.id); }}>Dismiss</button>
                  </div>
                )
              )}
              <div className="desk-note-time">{formatTime(deskNote.created_at_utc)}</div>
            </div>
          );
        })}

        {desktopFiles.map((desktopFile) => (
          <div
            key={desktopFile.id}
            className={`desktop-icon desktop-file-icon workstation-desktop-file ${selectedIcon === desktopFile.id ? "selected" : ""}`}
            style={{ position: "absolute", left: desktopFile.x, top: Math.min(desktopFile.y, 72) }}
            onClick={(event) => { event.stopPropagation(); onSelectDesktopFile(desktopFile.id); }}
            onDoubleClick={(event) => { event.stopPropagation(); onOpenDesktopFile(desktopFile.id); }}
            onContextMenu={(event) => { event.preventDefault(); event.stopPropagation(); onOpenDesktopFileContextMenu(event.clientX, event.clientY, desktopFile.id); }}
            onMouseDown={(event) => { if (event.button !== 0) return; event.stopPropagation(); onStartDesktopFileDrag(event, desktopFile); }}
          >
            <div className="desktop-icon-img">
              <svg width="40" height="40" viewBox="0 0 48 48" fill="none">
                <path d="M12 6H30L36 12V42H12V6Z" fill="#F4EFE6" stroke="#4A453D" strokeWidth="2" />
                <path d="M30 6L36 12H30V6Z" fill="#D4D0C8" stroke="#4A453D" strokeWidth="1.5" />
                <line x1="18" y1="20" x2="30" y2="20" stroke="#B6A48C" strokeWidth="1.5" />
                <line x1="18" y1="25" x2="30" y2="25" stroke="#B6A48C" strokeWidth="1.5" />
                <line x1="18" y1="30" x2="28" y2="30" stroke="#B6A48C" strokeWidth="1.5" />
              </svg>
            </div>
            <span className="desktop-icon-label">{desktopFile.name.length > 10 ? `${desktopFile.name.slice(0, 8)}...` : desktopFile.name}</span>
          </div>
        ))}
      </div>

      {/* Morph bubble — bottom right */}
      {morphBubble}

      {/* Floating windows (all apps including workspace) */}
      <div className="workstation-float-layer">
        {floatingLayer}
      </div>
    </div>
  );
}

import type { MouseEvent as ReactMouseEvent } from "react";

import type { DeskNote } from "../../wailsjs/go/main/HavenApp";
import type { DesktopFile, Wallpaper } from "../lib/haven";
import { MorphGlyph, formatTime, wallpaperBackgroundImage } from "../lib/haven";

interface DesktopSurfaceProps {
  wallpaper: Wallpaper;
  selectedIcon: string | null;
  presenceState: string;
  presenceStatusText: string;
  presenceDetailText: string;
  deskNotes: DeskNote[];
  deskNotePositions: Record<string, { x: number; y: number }>;
  desktopFiles: DesktopFile[];
  executingDeskNoteIDs: Record<string, boolean>;
  onDesktopClick: () => void;
  onDesktopContextMenu: (x: number, y: number) => void;
  onExecuteDeskNote: (noteID: string) => void;
  onDismissDeskNote: (noteID: string) => void;
  onSelectDesktopFile: (id: string) => void;
  onOpenDesktopFile: (fileID: string) => void;
  onOpenDesktopFileContextMenu: (x: number, y: number, fileID: string) => void;
  onStartDesktopFileDrag: (event: ReactMouseEvent<HTMLDivElement>, file: DesktopFile) => void;
  onStartDeskNoteDrag: (event: ReactMouseEvent<HTMLDivElement>, noteID: string) => void;
  children: React.ReactNode;
}

export default function DesktopSurface({
  wallpaper,
  selectedIcon,
  presenceState,
  presenceStatusText,
  presenceDetailText,
  deskNotes,
  deskNotePositions,
  desktopFiles,
  executingDeskNoteIDs,
  onDesktopClick,
  onDesktopContextMenu,
  onExecuteDeskNote,
  onDismissDeskNote,
  onSelectDesktopFile,
  onOpenDesktopFile,
  onOpenDesktopFileContextMenu,
  onStartDesktopFileDrag,
  onStartDeskNoteDrag,
  children,
}: DesktopSurfaceProps) {
  return (
    <div
      className="desktop"
      onClick={onDesktopClick}
      onContextMenu={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onDesktopContextMenu(event.clientX, event.clientY);
      }}
      style={{ backgroundImage: wallpaperBackgroundImage(wallpaper) }}
    >
      <div className={`morph-avatar morph-avatar-${presenceState}`} title={presenceStatusText} style={{ right: 48, bottom: 80, left: "auto", top: "auto", position: "absolute" }}>
        <div className={`morph-presence-card ${presenceState === "idle" ? "idle" : ""}`}>
          <div className="morph-presence-kicker">Morph</div>
          <div className="morph-presence-status">{presenceStatusText}</div>
          {presenceDetailText && <div className="morph-presence-detail">{presenceDetailText}</div>}
        </div>
        <div className="morph-avatar-orb-shell">
          <MorphGlyph size={72} className="morph-avatar-orb" />
        </div>
      </div>

      {deskNotes.map((deskNote, index) => {
        const position = deskNotePositions[deskNote.id] || { x: 60 + index * 24, y: 60 + index * 24 };
        return (
          <div
            key={deskNote.id}
            className={`desk-note desk-note-${deskNote.kind}`}
            style={{
              left: position.x,
              top: position.y,
              zIndex: 11 + deskNotes.length - index,
              transform: `rotate(${index % 2 === 0 ? "-1.3deg" : "1.1deg"})`,
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
              <div className="desk-note-executed">
                Working in Morph
              </div>
            ) : (
              <div className="desk-note-actions">
                <button
                  className="desk-note-action desk-note-action-primary"
                  type="button"
                  disabled={!!executingDeskNoteIDs[deskNote.id]}
                  onClick={(event) => {
                    event.stopPropagation();
                    onExecuteDeskNote(deskNote.id);
                  }}
                >
                  {executingDeskNoteIDs[deskNote.id] ? "Starting..." : (deskNote.action.label || "Yes")}
                </button>
                <button
                  className="desk-note-action desk-note-action-secondary"
                  type="button"
                  onClick={(event) => {
                    event.stopPropagation();
                    onDismissDeskNote(deskNote.id);
                  }}
                >
                  Dismiss
                </button>
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
          className={`desktop-icon desktop-file-icon ${selectedIcon === desktopFile.id ? "selected" : ""}`}
          style={{ position: "absolute", left: desktopFile.x, top: desktopFile.y }}
          onClick={(event) => { event.stopPropagation(); onSelectDesktopFile(desktopFile.id); }}
          onDoubleClick={(event) => { event.stopPropagation(); onOpenDesktopFile(desktopFile.id); }}
          onContextMenu={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onOpenDesktopFileContextMenu(event.clientX, event.clientY, desktopFile.id);
          }}
          onMouseDown={(event) => {
            if (event.button !== 0) return;
            event.stopPropagation();
            onStartDesktopFileDrag(event, desktopFile);
          }}
        >
          <div className="desktop-icon-img">
            <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
              <path d="M12 6H30L36 12V42H12V6Z" fill="#F2EFE9" stroke="#A09C94" strokeWidth="1.5" />
              <path d="M30 6L36 12H30V6Z" fill="#D4D0C8" stroke="#A09C94" strokeWidth="1" />
              <line x1="18" y1="20" x2="30" y2="20" stroke="#C8C4BC" strokeWidth="1" />
              <line x1="18" y1="25" x2="30" y2="25" stroke="#C8C4BC" strokeWidth="1" />
              <line x1="18" y1="30" x2="28" y2="30" stroke="#C8C4BC" strokeWidth="1" />
            </svg>
          </div>
          <span className="desktop-icon-label">{desktopFile.name.length > 12 ? `${desktopFile.name.slice(0, 10)}...` : desktopFile.name}</span>
        </div>
      ))}

      {children}
    </div>
  );
}

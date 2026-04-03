import { useEffect, useRef, useState } from "react";

import { WINDOW_MIN_SIZES, type WinState } from "../lib/haven";

type ResizeEdge = "e" | "s" | "se" | "w" | "n" | "nw" | "ne" | "sw";

interface HavenWindowProps {
  win: WinState;
  focused: boolean;
  onFocus: () => void;
  onClose: () => void;
  onCollapse: () => void;
  onDrag: (x: number, y: number) => void;
  onResize: (x: number, y: number, w: number, h: number) => void;
  children: React.ReactNode;
}

export default function HavenWindow({
  win,
  focused,
  onFocus,
  onClose,
  onCollapse,
  onDrag,
  onResize,
  children,
}: HavenWindowProps) {
  const [dragging, setDragging] = useState(false);
  const [resizing, setResizing] = useState<ResizeEdge | null>(null);
  const offset = useRef({ x: 0, y: 0 });
  const resizeStart = useRef({ mx: 0, my: 0, x: 0, y: 0, w: 0, h: 0 });

  useEffect(() => {
    if (!dragging) return;
    const handleMove = (event: MouseEvent) => onDrag(event.clientX - offset.current.x, event.clientY - offset.current.y);
    const handleUp = () => setDragging(false);
    document.addEventListener("mousemove", handleMove);
    document.addEventListener("mouseup", handleUp);
    return () => {
      document.removeEventListener("mousemove", handleMove);
      document.removeEventListener("mouseup", handleUp);
    };
  }, [dragging, onDrag]);

  useEffect(() => {
    if (!resizing) return;

    const mins = WINDOW_MIN_SIZES[win.id] || { w: 200, h: 120 };
    const handleMove = (event: MouseEvent) => {
      const start = resizeStart.current;
      const dx = event.clientX - start.mx;
      const dy = event.clientY - start.my;
      let nextX = start.x;
      let nextY = start.y;
      let nextW = start.w;
      let nextH = start.h;

      if (resizing.includes("e")) nextW = Math.max(mins.w, start.w + dx);
      if (resizing.includes("s")) nextH = Math.max(mins.h, start.h + dy);
      if (resizing === "w" || resizing === "nw" || resizing === "sw") {
        const resizedWidth = Math.max(mins.w, start.w - dx);
        nextX = start.x + start.w - resizedWidth;
        nextW = resizedWidth;
      }
      if (resizing === "n" || resizing === "nw" || resizing === "ne") {
        const resizedHeight = Math.max(mins.h, start.h - dy);
        nextY = start.y + start.h - resizedHeight;
        nextH = resizedHeight;
      }

      onResize(nextX, nextY, nextW, nextH);
    };

    const handleUp = () => setResizing(null);
    document.addEventListener("mousemove", handleMove);
    document.addEventListener("mouseup", handleUp);
    return () => {
      document.removeEventListener("mousemove", handleMove);
      document.removeEventListener("mouseup", handleUp);
    };
  }, [onResize, resizing, win.id]);

  const startResize = (edge: ResizeEdge) => (event: React.MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();
    onFocus();
    resizeStart.current = { mx: event.clientX, my: event.clientY, x: win.x, y: win.y, w: win.w, h: win.h };
    setResizing(edge);
  };

  return (
    <div className={`window ${focused ? "focused" : ""}`} style={{ left: win.x, top: win.y, width: win.w, zIndex: win.z }} onMouseDown={onFocus}>
      <div
        className="window-titlebar"
        onMouseDown={(event) => {
          if ((event.target as HTMLElement).closest(".window-closebox, .window-collapsebox")) return;
          event.preventDefault();
          onFocus();
          setDragging(true);
          offset.current = { x: event.clientX - win.x, y: event.clientY - win.y };
        }}
      >
        <div className="window-closebox" onClick={onClose}>&times;</div>
        <div className="window-title">{win.title}</div>
        <div className="window-collapsebox" onClick={onCollapse}>&ndash;</div>
      </div>
      {!win.collapsed && <div className="window-body" style={{ height: win.h - 24 }}>{children}</div>}
      {!win.collapsed && (
        <>
          <div className="win-resize win-resize-n" onMouseDown={startResize("n")} />
          <div className="win-resize win-resize-s" onMouseDown={startResize("s")} />
          <div className="win-resize win-resize-e" onMouseDown={startResize("e")} />
          <div className="win-resize win-resize-w" onMouseDown={startResize("w")} />
          <div className="win-resize win-resize-nw" onMouseDown={startResize("nw")} />
          <div className="win-resize win-resize-ne" onMouseDown={startResize("ne")} />
          <div className="win-resize win-resize-sw" onMouseDown={startResize("sw")} />
          <div className="win-resize win-resize-se" onMouseDown={startResize("se")} />
        </>
      )}
    </div>
  );
}

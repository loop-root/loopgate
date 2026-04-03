import { useEffect, useRef, useState } from "react";

import { EventsOn } from "../../../wailsjs/runtime/runtime";
import {
  ListPaintings,
  PaintSaveArtwork,
  type PaintArtworkSummary,
  type PaintPoint,
  type PaintSaveRequest,
  type PaintStroke,
} from "../../../wailsjs/go/main/HavenApp";
import { formatTime } from "../../lib/haven";

const CANVAS_WIDTH = 960;
const CANVAS_HEIGHT = 540;
const DEFAULT_BACKGROUND = "#F6F1E7";
const DEFAULT_BRUSH_COLOR = "#8E6C4B";
const BRUSH_COLORS = ["#8E6C4B", "#4D7FA0", "#4E8C5E", "#B84A4A", "#C8962E", "#3F2D1F"];

interface PaintWindowProps {
  onToast: (title: string, message: string, variant?: "info" | "success" | "warning") => void;
}

function svgDataURL(svgContent: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svgContent)}`;
}

function buildStrokePath(points: PaintPoint[]): string {
  if (!points.length) return "";
  return points.map((point, index) => `${index === 0 ? "M" : "L"} ${point.x.toFixed(2)} ${point.y.toFixed(2)}`).join(" ");
}

function pointFromPointer(event: React.PointerEvent<SVGSVGElement>, svgElement: SVGSVGElement): PaintPoint {
  const bounds = svgElement.getBoundingClientRect();
  return {
    x: ((event.clientX - bounds.left) / bounds.width) * CANVAS_WIDTH,
    y: ((event.clientY - bounds.top) / bounds.height) * CANVAS_HEIGHT,
  };
}

export default function PaintWindow({ onToast }: PaintWindowProps) {
  const [strokes, setStrokes] = useState<PaintStroke[]>([]);
  const [paintings, setPaintings] = useState<PaintArtworkSummary[]>([]);
  const [galleryLoading, setGalleryLoading] = useState(false);
  const [galleryError, setGalleryError] = useState("");
  const [saving, setSaving] = useState(false);
  const [title, setTitle] = useState("");
  const [brushColor, setBrushColor] = useState(DEFAULT_BRUSH_COLOR);
  const [brushWidth, setBrushWidth] = useState(6);

  const svgRef = useRef<SVGSVGElement | null>(null);
  const activeStrokeIndexRef = useRef<number | null>(null);

  const refreshGallery = async () => {
    setGalleryLoading(true);
    setGalleryError("");
    try {
      const entries = await ListPaintings();
      setPaintings(entries || []);
    } catch (errorValue) {
      setGalleryError(String(errorValue));
    } finally {
      setGalleryLoading(false);
    }
  };

  useEffect(() => {
    void refreshGallery();

    const unsubscribe = EventsOn("haven:file_changed", (...args: unknown[]) => {
      const data = (args[0] as Record<string, unknown>) || {};
      const path = String(data.path || "");
      if (path.startsWith("outputs/paintings/") || path.startsWith("artifacts/paintings/")) {
        void refreshGallery();
      }
    });

    return () => {
      unsubscribe();
    };
  }, []);

  const beginStroke = (event: React.PointerEvent<SVGSVGElement>) => {
    if (event.button !== 0 || !svgRef.current) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    const nextPoint = pointFromPointer(event, svgRef.current);
    setStrokes((previous) => {
      activeStrokeIndexRef.current = previous.length;
      return [...previous, { color: brushColor, width: brushWidth, points: [nextPoint] }];
    });
  };

  const extendStroke = (event: React.PointerEvent<SVGSVGElement>) => {
    if (!svgRef.current || activeStrokeIndexRef.current === null) return;
    const nextPoint = pointFromPointer(event, svgRef.current);
    setStrokes((previous) => {
      const strokeIndex = activeStrokeIndexRef.current;
      if (strokeIndex === null || !previous[strokeIndex]) return previous;

      const currentStroke = previous[strokeIndex];
      const lastPoint = currentStroke.points[currentStroke.points.length - 1];
      if (lastPoint) {
        const dx = nextPoint.x - lastPoint.x;
        const dy = nextPoint.y - lastPoint.y;
        if ((dx * dx) + (dy * dy) < 1) {
          return previous;
        }
      }

      const updatedStrokes = [...previous];
      updatedStrokes[strokeIndex] = {
        ...currentStroke,
        points: [...currentStroke.points, nextPoint],
      };
      return updatedStrokes;
    });
  };

  const endStroke = (event?: React.PointerEvent<SVGSVGElement>) => {
    if (event) {
      event.preventDefault();
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId);
      }
    }
    activeStrokeIndexRef.current = null;
  };

  const handleSave = async () => {
    if (strokes.length === 0 || saving) return;

    const request: PaintSaveRequest = {
      title,
      width: CANVAS_WIDTH,
      height: CANVAS_HEIGHT,
      background: DEFAULT_BACKGROUND,
      strokes,
    };

    setSaving(true);
    try {
      const response = await PaintSaveArtwork(request);
      if (response.error) {
        onToast("Paint save failed", response.error, "warning");
        return;
      }
      onToast("Painting saved", `${response.title} is now in Haven's artifacts.`, "success");
      void refreshGallery();
    } catch (errorValue) {
      onToast("Paint save failed", String(errorValue), "warning");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="paint-app">
      <div className="app-hero paint-hero">
        <div className="app-kicker">Paint</div>
        <div className="app-title">A small studio for sketches, diagrams, and little traces.</div>
        <div className="app-subtitle">
          Morph can leave something visual behind here without reaching for a generic shell command.
        </div>
        <div className="app-summary-pills">
          <span className="app-summary-pill">{strokes.length} stroke{strokes.length === 1 ? "" : "s"}</span>
          <span className="app-summary-pill">{paintings.length} saved</span>
          <span className="app-summary-pill">{brushWidth}px brush</span>
        </div>
      </div>

      <div className="paint-toolbar">
        <div className="paint-toolbar-left">
          <input
            className="paint-title-input"
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            placeholder="Untitled Painting"
            maxLength={80}
          />

          <div className="paint-color-row">
            {BRUSH_COLORS.map((color) => (
              <button
                key={color}
                className={`paint-color-chip ${brushColor === color ? "active" : ""}`}
                style={{ backgroundColor: color }}
                onClick={() => setBrushColor(color)}
                title={`Brush color ${color}`}
              />
            ))}
          </div>

          <label className="paint-brush-size">
            Brush
            <input
              type="range"
              min={2}
              max={18}
              value={brushWidth}
              onChange={(event) => setBrushWidth(Number(event.target.value))}
            />
            <span>{brushWidth}px</span>
          </label>
        </div>

        <div className="paint-toolbar-right">
          <button className="ws-tb-btn" disabled={strokes.length === 0} onClick={() => setStrokes([])}>Clear</button>
          <button className="ws-tb-btn" disabled={strokes.length === 0 || saving} onClick={() => { void handleSave(); }}>
            {saving ? "Saving..." : "Save"}
          </button>
        </div>
      </div>

      <div className="paint-body">
        <div className="paint-canvas-panel">
          <div className="paint-canvas-frame">
            <svg
              ref={svgRef}
              className="paint-canvas"
              viewBox={`0 0 ${CANVAS_WIDTH} ${CANVAS_HEIGHT}`}
              onPointerDown={beginStroke}
              onPointerMove={extendStroke}
              onPointerUp={endStroke}
              onPointerLeave={endStroke}
              onPointerCancel={endStroke}
            >
              <rect x="0" y="0" width={CANVAS_WIDTH} height={CANVAS_HEIGHT} fill={DEFAULT_BACKGROUND} />
              {strokes.map((stroke, strokeIndex) => (
                stroke.points.length === 1 ? (
                  <circle
                    key={`${strokeIndex}-dot`}
                    cx={stroke.points[0].x}
                    cy={stroke.points[0].y}
                    r={stroke.width / 2}
                    fill={stroke.color}
                  />
                ) : (
                  <path
                    key={`${strokeIndex}-path`}
                    d={buildStrokePath(stroke.points)}
                    fill="none"
                    stroke={stroke.color}
                    strokeWidth={stroke.width}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                )
              ))}
            </svg>
          </div>
          <div className="paint-canvas-caption">
            A quiet place for sketches, diagrams, and little traces Morph can leave behind.
          </div>
        </div>

        <div className="paint-sidebar">
          <div className="paint-sidebar-header">
            <div>
              <div className="paint-sidebar-title">Gallery</div>
              <div className="paint-sidebar-subtitle">Saved in Haven artifacts</div>
            </div>
            <button className="ws-tb-btn" onClick={() => { void refreshGallery(); }} title="Refresh gallery">↻</button>
          </div>

          {galleryError && <div className="paint-gallery-error">{galleryError}</div>}

          <div className="paint-gallery-list">
            {galleryLoading ? (
              <div className="paint-empty">Loading gallery...</div>
            ) : paintings.length === 0 ? (
              <div className="paint-empty">
                <div className="paint-empty-title">Nothing saved yet</div>
                <div className="paint-empty-copy">Leave a sketch behind and Haven will start to feel a little more lived in.</div>
              </div>
            ) : (
              paintings.map((painting) => (
                <div key={painting.path} className="paint-gallery-card">
                  <div className="paint-gallery-thumb">
                    <img src={svgDataURL(painting.preview_svg)} alt={painting.title} />
                  </div>
                  <div className="paint-gallery-meta">
                    <div className="paint-gallery-title">{painting.title}</div>
                    <div className="paint-gallery-time">{formatTime(painting.updated_at_utc)}</div>
                    <div className="paint-gallery-path">{painting.path}</div>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

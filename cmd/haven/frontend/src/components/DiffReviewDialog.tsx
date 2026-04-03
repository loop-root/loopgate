import type { DiffLine } from "../../wailsjs/go/main/HavenApp";

export interface DiffReviewState {
  path: string;
  lines: DiffLine[];
  hasChanges: boolean;
  loading: boolean;
  error: string;
}

interface DiffReviewDialogProps {
  reviewState: DiffReviewState | null;
  busyAction?: "export" | "discard" | null;
  onClose: () => void;
  onExport: () => void;
  onDiscard: () => void;
}

export default function DiffReviewDialog({
  reviewState,
  busyAction = null,
  onClose,
  onExport,
  onDiscard,
}: DiffReviewDialogProps) {
  if (!reviewState) return null;

  const canDiscard = !reviewState.loading && !reviewState.error && reviewState.hasChanges;

  return (
    <div className="system-modal-backdrop">
      <div className="system-modal system-modal-wide">
        <div className="system-modal-header">
          <div>
            <div className="system-modal-title">Review Changes</div>
            <div className="system-modal-desc">Inspect the file before you export it to your computer or discard the Haven copy.</div>
          </div>
          <button className="system-modal-close" onClick={onClose} disabled={busyAction !== null}>&times;</button>
        </div>
        <div className="system-modal-path">{reviewState.path}</div>
        <div className="diff-review-panel">
          {reviewState.loading ? (
            <div className="diff-review-empty">Loading diff...</div>
          ) : reviewState.error ? (
            <div className="diff-review-empty">
              <div className="diff-review-empty-title">No original baseline available</div>
              <div className="diff-review-empty-copy">{reviewState.error}</div>
            </div>
          ) : !reviewState.hasChanges ? (
            <div className="diff-review-empty">
              <div className="diff-review-empty-title">No changes detected</div>
              <div className="diff-review-empty-copy">This Haven file still matches its original imported version.</div>
            </div>
          ) : (
            <div className="diff-review-lines">
              {reviewState.lines.map((line, index) => (
                <div key={`${line.type}-${index}`} className={`diff-review-line diff-review-line-${line.type}`}>
                  <span className="diff-review-gutter">
                    {line.type === "add" ? "+" : line.type === "remove" ? "-" : line.type === "header" ? ">" : " "}
                  </span>
                  <span className="diff-review-text">{line.text || " "}</span>
                </div>
              ))}
            </div>
          )}
        </div>
        <div className="system-modal-actions">
          <button className="retro-btn" onClick={onClose} disabled={busyAction !== null}>Close</button>
          <button className="retro-btn deny" onClick={onDiscard} disabled={!canDiscard || busyAction !== null}>
            {busyAction === "discard" ? "Discarding..." : "Discard Changes"}
          </button>
          <button className="retro-btn primary" onClick={onExport} disabled={reviewState.loading || busyAction !== null}>
            {busyAction === "export" ? "Exporting..." : "Export to Computer..."}
          </button>
        </div>
      </div>
    </div>
  );
}

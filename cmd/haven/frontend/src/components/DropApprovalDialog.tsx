import type { PendingDrop } from "../lib/haven";

interface DropApprovalDialogProps {
  pendingDrop: PendingDrop | null;
  onAllow: () => void;
  onDeny: () => void;
}

export default function DropApprovalDialog({ pendingDrop, onAllow, onDeny }: DropApprovalDialogProps) {
  if (!pendingDrop) return null;

  return (
    <div className="approval-modal-backdrop">
      <div className="approval-modal">
        <div className="approval-modal-icon">
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
            <path d="M12 6H30L36 12V42H12V6Z" fill="#F2EFE9" stroke="#4D7FA0" strokeWidth="2" />
            <path d="M30 6L36 12H30V6Z" fill="#D4D0C8" stroke="#4D7FA0" strokeWidth="1.5" />
            <path d="M20 24L24 28L28 20" stroke="#4E8C5E" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </div>
        <div className="approval-modal-body">
          <div className="approval-modal-kicker">Import into Haven</div>
          <div className="approval-modal-title">Import {pendingDrop.paths.length === 1 ? "file" : `${pendingDrop.paths.length} files`} into Haven?</div>
          <div className="approval-modal-desc">
            This creates a copy in Haven's workspace. Your originals won't be touched.
            Morph will have full read/write access to the copy.
          </div>
          <div className="approval-modal-details" style={{ textAlign: "left" }}>
            {pendingDrop.paths.map((path, index) => (
              <div key={index} className="approval-detail-row">
                <span className="approval-detail-label">File</span>
                <span className="approval-detail-value">{path.split("/").pop() || path}</span>
              </div>
            ))}
          </div>
        </div>
        <div className="approval-modal-actions">
          <button className="retro-btn deny" onClick={onDeny}>Cancel</button>
          <button className="retro-btn allow" onClick={onAllow}>Import</button>
        </div>
      </div>
    </div>
  );
}

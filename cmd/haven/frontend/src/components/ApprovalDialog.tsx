import { useState } from "react";

import { IconLoopgate, approvalDescription, approvalTitle } from "../lib/haven";

interface ApprovalDialogProps {
  capability: string;
  onAllow: () => void;
  onDeny: () => void;
}

export default function ApprovalDialog({ capability, onAllow, onDeny }: ApprovalDialogProps) {
  const [showDetails, setShowDetails] = useState(false);

  return (
    <div className="approval-modal-backdrop">
      <div className="approval-modal">
        <div className="approval-modal-icon">
          <IconLoopgate />
        </div>
        <div className="approval-modal-body">
          <div className="approval-modal-kicker">Permission Request</div>
          <div className="approval-modal-title">{approvalTitle(capability)}</div>
          <div className="approval-modal-desc">{approvalDescription(capability)}</div>
          {showDetails && (
            <div className="approval-modal-details">
              <div className="approval-detail-row">
                <span className="approval-detail-label">Capability</span>
                <span className="approval-detail-value">{capability}</span>
              </div>
              <div className="approval-detail-row">
                <span className="approval-detail-label">Mediated by</span>
                <span className="approval-detail-value">Loopgate security kernel</span>
              </div>
            </div>
          )}
          <div className="approval-modal-toggle" onClick={() => setShowDetails(!showDetails)}>
            {showDetails ? "\u25BE Hide Details" : "\u25B8 Show Details"}
          </div>
        </div>
        <div className="approval-modal-actions">
          <button className="retro-btn deny" onClick={onDeny}>Deny</button>
          <button className="retro-btn allow" onClick={onAllow}>Allow</button>
        </div>
      </div>
    </div>
  );
}

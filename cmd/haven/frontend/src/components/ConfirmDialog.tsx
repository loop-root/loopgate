interface ConfirmDialogProps {
  title: string;
  description: string;
  detail?: string;
  confirmLabel: string;
  cancelLabel?: string;
  destructive?: boolean;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmDialog({
  title,
  description,
  detail,
  confirmLabel,
  cancelLabel = "Cancel",
  destructive = false,
  busy = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  return (
    <div className="system-modal-backdrop">
      <div className="system-modal">
        <div className="system-modal-body">
          <div className="system-modal-kicker">{destructive ? "Confirm Removal" : "Confirm Action"}</div>
          <div className="system-modal-title">{title}</div>
          <div className="system-modal-desc">{description}</div>
          {detail && <div className="system-modal-detail">{detail}</div>}
        </div>
        <div className="system-modal-actions">
          <button className="retro-btn" onClick={onCancel} disabled={busy}>{cancelLabel}</button>
          <button className={`retro-btn ${destructive ? "deny" : "primary"}`} onClick={onConfirm} disabled={busy}>
            {busy ? "Working..." : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

import { useEffect, useRef } from "react";

interface TextInputDialogProps {
  title: string;
  description: string;
  value: string;
  placeholder?: string;
  confirmLabel: string;
  cancelLabel?: string;
  busy?: boolean;
  onChange: (value: string) => void;
  onConfirm: () => void;
  onCancel: () => void;
}

export default function TextInputDialog({
  title,
  description,
  value,
  placeholder,
  confirmLabel,
  cancelLabel = "Cancel",
  busy = false,
  onChange,
  onConfirm,
  onCancel,
}: TextInputDialogProps) {
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  return (
    <div className="system-modal-backdrop">
      <div className="system-modal">
        <div className="system-modal-body">
          <div className="system-modal-kicker">Rename</div>
          <div className="system-modal-title">{title}</div>
          <div className="system-modal-desc">{description}</div>
          <input
            ref={inputRef}
            className="system-modal-input"
            value={value}
            placeholder={placeholder}
            onChange={(event) => onChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && value.trim()) onConfirm();
              if (event.key === "Escape") onCancel();
            }}
          />
        </div>
        <div className="system-modal-actions">
          <button className="retro-btn" onClick={onCancel} disabled={busy}>{cancelLabel}</button>
          <button className="retro-btn primary" onClick={onConfirm} disabled={busy || !value.trim()}>
            {busy ? "Saving..." : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

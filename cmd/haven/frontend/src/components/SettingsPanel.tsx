import { useEffect, useState } from "react";

import { BrowserOpenURL } from "../../wailsjs/wailsjs/runtime/runtime";
import {
  GetSettings,
  GetSharedFolderStatus,
  GetModelSettings,
  GetLoopgateDiagnosticLogging,
  SaveSettings,
  SaveLoopgateDiagnosticLogging,
  ClearLoopgateDiagnosticLoggingOverride,
  SyncSharedFolder,
  type SharedFolderStatusResponse,
  type ModelSettingsResponse,
  type LoopgateDiagnosticLoggingStatus,
} from "../../wailsjs/go/main/HavenApp";
import type { Wallpaper } from "../lib/haven";
import { wallpaperPreviewImage } from "../lib/haven";
import ModelProviderSection from "./settings/ModelProviderSection";

export interface SettingsPanelProps {
  wallpaper: Wallpaper;
  wallpapers: Wallpaper[];
  onWallpaperChange: (wallpaper: Wallpaper) => void;
}

export default function SettingsPanel({ wallpaper, wallpapers, onWallpaperChange }: SettingsPanelProps) {
  const [morphName, setMorphName] = useState("");
  const [idleEnabled, setIdleEnabled] = useState(true);
  const [ambientEnabled, setAmbientEnabled] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [sharedFolder, setSharedFolder] = useState<SharedFolderStatusResponse | null>(null);
  const [sharedFolderBusy, setSharedFolderBusy] = useState(false);
  const [modelSettings, setModelSettings] = useState<ModelSettingsResponse | null>(null);
  const [diag, setDiag] = useState<LoopgateDiagnosticLoggingStatus | null>(null);
  const [diagEnabled, setDiagEnabled] = useState(true);
  const [diagLevel, setDiagLevel] = useState("debug");
  const [diagSaving, setDiagSaving] = useState(false);
  const [diagSaved, setDiagSaved] = useState(false);
  const [diagErr, setDiagErr] = useState("");

  const reloadDiagnostic = () => {
    GetLoopgateDiagnosticLogging()
      .then((status) => {
        setDiag(status);
        if (!status.config_load_error) {
          setDiagEnabled(status.enabled);
          setDiagLevel(status.default_level || "debug");
        }
      })
      .catch(() => setDiag(null));
  };

  useEffect(() => {
    Promise.all([GetSettings(), GetSharedFolderStatus(), GetModelSettings(), GetLoopgateDiagnosticLogging()])
      .then(([settings, sharedFolderStatus, modelSettingsResponse, diagStatus]) => {
        setMorphName(settings.morph_name);
        setIdleEnabled(settings.idle_enabled);
        setAmbientEnabled(settings.ambient_enabled);
        setSharedFolder(sharedFolderStatus);
        setModelSettings(modelSettingsResponse);
        setDiag(diagStatus);
        if (!diagStatus.config_load_error) {
          setDiagEnabled(diagStatus.enabled);
          setDiagLevel(diagStatus.default_level || "debug");
        }
        setLoaded(true);
      })
      .catch(() => {
        setModelSettings({
          current_model: "unknown",
          provider_name: "unknown",
          base_url: "",
          available_models: [],
          mode: "local",
          has_cloud_credential: false,
          local_base_url: "http://localhost:11434/v1",
        });
        reloadDiagnostic();
        setLoaded(true);
      });
  }, []);

  const openDiagLogFolder = () => {
    if (!diag?.log_directory_host_path) return;
    const logURL = new URL(`file://${diag.log_directory_host_path}`);
    BrowserOpenURL(logURL.toString());
  };

  const handleSaveDiagnostic = async () => {
    setDiagSaving(true);
    setDiagErr("");
    try {
      const result = await SaveLoopgateDiagnosticLogging(diagEnabled, diagLevel);
      if (result.success) {
        setDiagSaved(true);
        reloadDiagnostic();
        setTimeout(() => setDiagSaved(false), 2000);
      } else if (result.error) {
        setDiagErr(result.error);
      }
    } finally {
      setDiagSaving(false);
    }
  };

  const handleClearDiagnosticOverride = async () => {
    setDiagSaving(true);
    setDiagErr("");
    try {
      const result = await ClearLoopgateDiagnosticLoggingOverride();
      if (result.success) {
        reloadDiagnostic();
      } else if (result.error) {
        setDiagErr(result.error);
      }
    } finally {
      setDiagSaving(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await SaveSettings({
        morph_name: morphName || "Morph",
        wallpaper: wallpaper.id,
        idle_enabled: idleEnabled,
        ambient_enabled: ambientEnabled,
      });
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch {
      // Keep current optimistic UI behavior for now.
    } finally {
      setSaving(false);
    }
  };

  const handleSyncSharedFolder = async () => {
    setSharedFolderBusy(true);
    try {
      const sharedFolderStatus = await SyncSharedFolder();
      setSharedFolder(sharedFolderStatus);
    } catch {
      // Keep settings usable even if shared-folder sync is unavailable.
    } finally {
      setSharedFolderBusy(false);
    }
  };

  const openSharedFolder = () => {
    if (!sharedFolder?.host_path) return;
    const sharedFolderURL = new URL(`file://${sharedFolder.host_path}`);
    BrowserOpenURL(sharedFolderURL.toString());
  };

  if (!loaded) {
    return <div className="settings-app"><div className="settings-loading">Loading settings...</div></div>;
  }

  return (
    <div className="settings-app">
      <div className="app-hero settings-hero">
        <div className="app-kicker">Settings</div>
        <div className="app-title">Shape how Haven feels and what Morph can easily reach.</div>
        <div className="app-subtitle">
          Identity, wallpaper, idle behavior, and the shared space all live here.
        </div>
        <div className="app-summary-pills">
          <span className="app-summary-pill">{morphName || "Morph"}</span>
          <span className="app-summary-pill">{wallpaper.name}</span>
          <span className="app-summary-pill">{idleEnabled ? "idle on" : "idle off"}</span>
          <span className="app-summary-pill">{ambientEnabled ? "full presence" : "focused help"}</span>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Identity</div>
        <label className="settings-label">Assistant name</label>
        <input className="settings-input" value={morphName} onChange={(event) => setMorphName(event.target.value)} placeholder="Morph" />
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Model</div>
        {modelSettings ? (
          <ModelProviderSection modelSettings={modelSettings} onModelSettingsChange={setModelSettings} />
        ) : (
          <div className="settings-hint">Loading model settings…</div>
        )}
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Appearance</div>
        <label className="settings-label">Desktop wallpaper</label>
        <div className="settings-wallpaper-grid">
          {wallpapers.map((nextWallpaper) => (
            <button
              key={nextWallpaper.id}
              className={`wallpaper-swatch ${wallpaper.id === nextWallpaper.id ? "active" : ""}`}
              type="button"
              onClick={() => onWallpaperChange(nextWallpaper)}
            >
              <span className="wallpaper-swatch-preview" style={{ backgroundImage: wallpaperPreviewImage(nextWallpaper) }} />
              <span className="wallpaper-swatch-copy">
                <span className="wallpaper-swatch-label">{nextWallpaper.name}</span>
                <span className="wallpaper-swatch-mood">{nextWallpaper.mood}</span>
              </span>
            </button>
          ))}
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Behavior</div>
        <label className="settings-checkbox">
          <input type="checkbox" checked={idleEnabled} onChange={(event) => setIdleEnabled(event.target.checked)} />
          Enable resident behavior
        </label>
        <div className="settings-hint">When Haven is quiet, Morph can keep up with open tasks and leave calm suggestions.</div>
        <label className="settings-checkbox">
          <input type="checkbox" checked={ambientEnabled} onChange={(event) => setAmbientEnabled(event.target.checked)} disabled={!idleEnabled} />
          Allow ambient journaling and art
        </label>
        <div className="settings-hint">Turn this off to keep Morph utility-first. For cloud models, this is the calmer default.</div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Loopgate debug logs</div>
        {diag?.config_load_error ? (
          <div className="settings-hint">Could not read runtime config: {diag.config_load_error}</div>
        ) : (
          <>
            <div className="settings-hint">
              Text logs under <code className="settings-inline-code">runtime/logs</code> (see{" "}
              <code className="settings-inline-code">config/runtime.yaml</code> → <code className="settings-inline-code">logging.diagnostic</code>
              ). Restart Loopgate after changing this. Socket and HTTP request lines go to <code className="settings-inline-code">socket.log</code> /{" "}
              <code className="settings-inline-code">client.log</code>; control-plane events to <code className="settings-inline-code">server.log</code>.
            </div>
            {diag?.override_active ? (
              <div className="settings-hint">
                A local override file is active (<code className="settings-inline-code">runtime/state/loopgate_diagnostic_logging.override.json</code>
                ), masking YAML for enabled/level until you clear it.
              </div>
            ) : null}
            <label className="settings-checkbox">
              <input type="checkbox" checked={diagEnabled} onChange={(event) => setDiagEnabled(event.target.checked)} />
              Enable diagnostic log files
            </label>
            <label className="settings-label">Log verbosity</label>
            <select className="settings-input" value={diagLevel} onChange={(event) => setDiagLevel(event.target.value)}>
              <option value="error">error</option>
              <option value="warn">warn</option>
              <option value="info">info</option>
              <option value="debug">debug</option>
              <option value="trace">trace (finest)</option>
            </select>
            <div className="settings-path">{diag?.log_directory_host_path || "—"}</div>
            {diagErr ? <div className="settings-hint">{diagErr}</div> : null}
            <div className="settings-inline-actions">
              <button className="retro-btn" type="button" disabled={diagSaving} onClick={handleSaveDiagnostic}>
                {diagSaved ? "Saved override" : diagSaving ? "Saving…" : "Save logging override"}
              </button>
              <button className="retro-btn" type="button" disabled={!diag?.log_directory_host_path} onClick={openDiagLogFolder}>
                Open log folder
              </button>
              <button
                className="retro-btn"
                type="button"
                disabled={diagSaving || !diag?.override_active}
                onClick={handleClearDiagnosticOverride}
              >
                Clear override (use YAML only)
              </button>
            </div>
          </>
        )}
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Shared Space</div>
        <label className="settings-label">Default shared folder</label>
        <div className="settings-path">{sharedFolder?.host_path || "~/Shared with Morph"}</div>
        <div className="settings-hint">
          Anything you place here is mirrored into Haven as <code className="settings-inline-code">shared</code>. Morph can browse that mirrored copy without asking you to drag files in one by one.
        </div>
        <div className="settings-shared-meta">
          <span>{sharedFolder?.mirror_ready ? `Ready in Haven as ${sharedFolder.sandbox_relative_path === "imports/shared" ? "shared" : sharedFolder.sandbox_relative_path}` : "Not mirrored yet"}</span>
          {sharedFolder?.mirror_ready && <span>{sharedFolder.entry_count} item{sharedFolder.entry_count === 1 ? "" : "s"}</span>}
        </div>
        <div className="settings-inline-actions">
          <button className="retro-btn" type="button" disabled={!sharedFolder?.host_path} onClick={openSharedFolder}>Open Folder</button>
          <button className="retro-btn" type="button" disabled={sharedFolderBusy} onClick={handleSyncSharedFolder}>
            {sharedFolderBusy ? "Syncing..." : "Sync Now"}
          </button>
        </div>
      </div>

      <div className="settings-actions">
        <button className="retro-btn primary" disabled={saving} onClick={handleSave}>
          {saved ? "Saved!" : saving ? "Saving..." : "Save"}
        </button>
      </div>
    </div>
  );
}

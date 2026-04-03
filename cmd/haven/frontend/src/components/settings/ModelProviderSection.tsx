import { useCallback, useEffect, useState } from "react";

import {
  DetectOllama,
  GetModelSettings,
  ListLocalModels,
  SaveModelProviderSettings,
  SaveModelSelection,
  type ModelSettingsResponse,
  type OllamaModel,
} from "../../../wailsjs/go/main/HavenApp";

/** Matches setup wizard default; extend as product adds first-class presets. */
const ANTHROPIC_MODEL_PRESETS: { id: string; label: string }[] = [
  { id: "claude-sonnet-4-5", label: "Claude Sonnet 4.5 (recommended)" },
  { id: "claude-3-5-sonnet-20241022", label: "Claude 3.5 Sonnet" },
  { id: "claude-3-5-haiku-20241022", label: "Claude 3.5 Haiku" },
];

const CUSTOM_ANTHROPIC = "__custom__";

export interface ModelProviderSectionProps {
  modelSettings: ModelSettingsResponse;
  onModelSettingsChange: (next: ModelSettingsResponse) => void;
}

type CloudWizardStep = "choose_provider" | "connect";

export default function ModelProviderSection({
  modelSettings,
  onModelSettingsChange,
}: ModelProviderSectionProps) {
  const isAnthropic = modelSettings.mode === "anthropic";

  const refresh = useCallback(async () => {
    const next = await GetModelSettings();
    onModelSettingsChange(next);
    return next;
  }, [onModelSettingsChange]);

  // --- Cloud switch flow (from local) ---
  const [cloudOpen, setCloudOpen] = useState(false);
  const [cloudStep, setCloudStep] = useState<CloudWizardStep>("choose_provider");
  const [cloudAnthropicModelChoice, setCloudAnthropicModelChoice] = useState(ANTHROPIC_MODEL_PRESETS[0].id);
  const [cloudAnthropicCustomId, setCloudAnthropicCustomId] = useState("");
  const [cloudApiKey, setCloudApiKey] = useState("");
  const [cloudBusy, setCloudBusy] = useState(false);
  const [cloudError, setCloudError] = useState("");

  // --- Local switch flow (from cloud) ---
  const [localOpen, setLocalOpen] = useState(false);
  const [localBaseURL, setLocalBaseURL] = useState(modelSettings.local_base_url || "http://localhost:11434/v1");
  const [localModels, setLocalModels] = useState<string[]>([]);
  const [localModelPick, setLocalModelPick] = useState("");
  const [localModelsLoading, setLocalModelsLoading] = useState(false);
  const [ollamaDetected, setOllamaDetected] = useState(false);
  const [localBusy, setLocalBusy] = useState(false);
  const [localError, setLocalError] = useState("");

  // --- Anthropic: change model / replace key (already on cloud) ---
  const [anthropicModelChoice, setAnthropicModelChoice] = useState(modelSettings.current_model);
  const [anthropicCustomId, setAnthropicCustomId] = useState("");
  const [showReplaceKey, setShowReplaceKey] = useState(false);
  const [replaceKeyValue, setReplaceKeyValue] = useState("");
  /** First-time key when config is anthropic but Loopgate has no stored secret yet. */
  const [bootstrapApiKey, setBootstrapApiKey] = useState("");
  const [anthropicModelBusy, setAnthropicModelBusy] = useState(false);
  const [anthropicKeyBusy, setAnthropicKeyBusy] = useState(false);
  const [anthropicBanner, setAnthropicBanner] = useState("");

  useEffect(() => {
    if (!isAnthropic) return;
    const known = ANTHROPIC_MODEL_PRESETS.some((p) => p.id === modelSettings.current_model);
    if (known) {
      setAnthropicModelChoice(modelSettings.current_model);
    } else {
      setAnthropicModelChoice(CUSTOM_ANTHROPIC);
      setAnthropicCustomId(modelSettings.current_model);
    }
  }, [isAnthropic, modelSettings.current_model]);

  useEffect(() => {
    if (localOpen) {
      setLocalBaseURL(modelSettings.local_base_url || "http://localhost:11434/v1");
    }
  }, [localOpen, modelSettings.local_base_url]);

  const resolvedCloudAnthropicModelId =
    cloudAnthropicModelChoice === CUSTOM_ANTHROPIC ? cloudAnthropicCustomId.trim() : cloudAnthropicModelChoice;

  const loadLocalModels = useCallback(async () => {
    setLocalModelsLoading(true);
    setLocalError("");
    try {
      const [detected, names] = await Promise.all([
        DetectOllama(),
        ListLocalModels(localBaseURL.trim()).catch(() => [] as string[]),
      ]);
      setOllamaDetected(detected);
      setLocalModels(names);
      if (names.length > 0) {
        setLocalModelPick((prev) => (prev && names.includes(prev) ? prev : names[0]));
      }
    } catch (e) {
      setLocalError(String(e));
    } finally {
      setLocalModelsLoading(false);
    }
  }, [localBaseURL]);

  useEffect(() => {
    if (!localOpen) return;
    let cancelled = false;
    (async () => {
      if (cancelled) return;
      await loadLocalModels();
    })();
    return () => {
      cancelled = true;
    };
  }, [localOpen, loadLocalModels]);

  const openCloudFlow = () => {
    setCloudError("");
    setCloudApiKey("");
    setCloudStep("choose_provider");
    setCloudAnthropicModelChoice(ANTHROPIC_MODEL_PRESETS[0].id);
    setCloudAnthropicCustomId("");
    setCloudOpen(true);
    setLocalOpen(false);
  };

  const openLocalFlow = () => {
    setLocalError("");
    setLocalOpen(true);
    setCloudOpen(false);
  };

  const cancelFlows = () => {
    setCloudOpen(false);
    setLocalOpen(false);
    setCloudApiKey("");
    setReplaceKeyValue("");
    setBootstrapApiKey("");
    setShowReplaceKey(false);
    setCloudError("");
    setLocalError("");
  };

  const handleCloudConnect = async () => {
    const modelId = resolvedCloudAnthropicModelId;
    if (!modelId) {
      setCloudError("Choose or enter a Claude model id.");
      return;
    }
    const key = cloudApiKey.trim();
    if (!key) {
      setCloudError("Paste your Anthropic API key once. Loopgate stores it in the system keychain — not in your project files.");
      return;
    }
    setCloudBusy(true);
    setCloudError("");
    try {
      const result = await SaveModelProviderSettings({
        mode: "anthropic",
        model_name: modelId,
        local_base_url: "",
        anthropic_api_key: key,
      });
      if (!result.success) {
        setCloudError(result.error || "Could not connect cloud provider.");
        return;
      }
      setCloudApiKey("");
      setCloudOpen(false);
      await refresh();
    } catch (e) {
      setCloudError(String(e));
    } finally {
      setCloudBusy(false);
    }
  };

  const handleLocalSwitch = async () => {
    const model = localModelPick.trim();
    if (!model) {
      setLocalError("Pick a local model, or check that Ollama is running and press Refresh.");
      return;
    }
    setLocalBusy(true);
    setLocalError("");
    try {
      const result = await SaveModelProviderSettings({
        mode: "local",
        model_name: model,
        local_base_url: localBaseURL.trim() || "http://localhost:11434/v1",
        anthropic_api_key: "",
      });
      if (!result.success) {
        setLocalError(result.error || "Could not switch to local models.");
        return;
      }
      setLocalOpen(false);
      await refresh();
    } catch (e) {
      setLocalError(String(e));
    } finally {
      setLocalBusy(false);
    }
  };

  const resolvedActiveAnthropicId =
    anthropicModelChoice === CUSTOM_ANTHROPIC ? anthropicCustomId.trim() : anthropicModelChoice;

  const handleAnthropicModelOnlySave = async () => {
    if (!resolvedActiveAnthropicId) {
      setAnthropicBanner("Enter a model id.");
      return;
    }
    if (resolvedActiveAnthropicId === modelSettings.current_model) {
      setAnthropicBanner("Already using this model.");
      setTimeout(() => setAnthropicBanner(""), 2000);
      return;
    }
    setAnthropicModelBusy(true);
    setAnthropicBanner("");
    try {
      const result = await SaveModelSelection({ model_name: resolvedActiveAnthropicId });
      if (!result.success) {
        setAnthropicBanner(result.error || "Could not update model.");
        return;
      }
      setAnthropicBanner("Updated. Takes effect on the next message.");
      setTimeout(() => setAnthropicBanner(""), 3500);
      await refresh();
    } catch (e) {
      setAnthropicBanner(String(e));
    } finally {
      setAnthropicModelBusy(false);
    }
  };

  const handleReplaceAnthropicKey = async () => {
    const key = replaceKeyValue.trim();
    if (!key) {
      setAnthropicBanner("Paste a new API key, or cancel.");
      return;
    }
    setAnthropicKeyBusy(true);
    setAnthropicBanner("");
    try {
      const result = await SaveModelProviderSettings({
        mode: "anthropic",
        model_name: modelSettings.current_model,
        local_base_url: "",
        anthropic_api_key: key,
      });
      if (!result.success) {
        setAnthropicBanner(result.error || "Could not update API key.");
        return;
      }
      setReplaceKeyValue("");
      setShowReplaceKey(false);
      setAnthropicBanner("New key saved in Keychain via Loopgate.");
      setTimeout(() => setAnthropicBanner(""), 3500);
      await refresh();
    } catch (e) {
      setAnthropicBanner(String(e));
    } finally {
      setAnthropicKeyBusy(false);
    }
  };

  const handleBootstrapAnthropicKey = async () => {
    const key = bootstrapApiKey.trim();
    if (!key) {
      setAnthropicBanner("Paste your Anthropic API key to finish setup.");
      return;
    }
    setAnthropicKeyBusy(true);
    setAnthropicBanner("");
    try {
      const result = await SaveModelProviderSettings({
        mode: "anthropic",
        model_name: modelSettings.current_model,
        local_base_url: "",
        anthropic_api_key: key,
      });
      if (!result.success) {
        setAnthropicBanner(result.error || "Could not save API key.");
        return;
      }
      setBootstrapApiKey("");
      setAnthropicBanner("Key saved in Keychain via Loopgate.");
      setTimeout(() => setAnthropicBanner(""), 3500);
      await refresh();
    } catch (e) {
      setAnthropicBanner(String(e));
    } finally {
      setAnthropicKeyBusy(false);
    }
  };

  // --- Local mode: pick from Ollama list ---
  const [localPickBusy, setLocalPickBusy] = useState(false);
  const [localPickSaved, setLocalPickSaved] = useState(false);
  const [selectedLocalModel, setSelectedLocalModel] = useState(modelSettings.current_model);

  useEffect(() => {
    setSelectedLocalModel(modelSettings.current_model);
  }, [modelSettings.current_model, modelSettings.mode]);

  const handleLocalModelSave = async () => {
    if (!selectedLocalModel || selectedLocalModel === modelSettings.current_model) return;
    setLocalPickBusy(true);
    try {
      const result = await SaveModelSelection({ model_name: selectedLocalModel });
      if (!result.success) {
        return;
      }
      setLocalPickSaved(true);
      setTimeout(() => setLocalPickSaved(false), 2000);
      await refresh();
    } finally {
      setLocalPickBusy(false);
    }
  };

  return (
    <>
      <div className="settings-model-summary">
        <div className="settings-model-summary-row">
          <span className="settings-model-pill">{isAnthropic ? "Cloud" : "Local"}</span>
          <span className="settings-model-summary-text">
            {isAnthropic
              ? "Anthropic · requests go through Loopgate"
              : `Ollama / OpenAI-compatible · ${modelSettings.provider_name}`}
          </span>
        </div>
        <div className="settings-model-active">
          Active model: <strong>{modelSettings.current_model}</strong>
        </div>
      </div>

      {!isAnthropic && (
        <>
          {modelSettings.available_models.length > 0 ? (
            <>
              <label className="settings-label">Installed models</label>
              <select
                className="settings-input"
                value={selectedLocalModel}
                onChange={(e) => setSelectedLocalModel(e.target.value)}
              >
                {modelSettings.available_models.map((m: OllamaModel) => (
                  <option key={m.name} value={m.name}>
                    {m.name}
                  </option>
                ))}
                {!modelSettings.available_models.some((m: OllamaModel) => m.name === selectedLocalModel) && (
                  <option value={selectedLocalModel}>{selectedLocalModel} (current)</option>
                )}
              </select>
              <div className="settings-hint">Changing the model takes effect on the next message.</div>
              {selectedLocalModel !== modelSettings.current_model && (
                <button
                  className="retro-btn settings-model-btn-margin"
                  type="button"
                  disabled={localPickBusy}
                  onClick={() => void handleLocalModelSave()}
                >
                  {localPickSaved ? "Switched!" : localPickBusy ? "Switching…" : "Switch model"}
                </button>
              )}
            </>
          ) : (
            <div className="settings-hint settings-model-spaced">
              No models were detected at your Ollama URL. If Ollama is running, use{" "}
              <strong>Use local models</strong> below to set the endpoint and refresh the list — or connect to Claude in
              the cloud.
            </div>
          )}

          <div className="settings-model-actions">
            <button className="retro-btn primary" type="button" onClick={openCloudFlow}>
              Use Claude in the cloud…
            </button>
            {!localOpen && (
              <button className="retro-btn" type="button" onClick={openLocalFlow}>
                Configure local endpoint…
              </button>
            )}
          </div>
        </>
      )}

      {isAnthropic && (
        <>
          <div className="settings-hint settings-model-spaced">
            Loopgate stores your Anthropic API key in the system keychain and uses it for outbound calls — Morph never
            keeps the raw key in project files.
          </div>
          {modelSettings.has_cloud_credential ? (
            <div className="settings-model-key-status">API key on file: Keychain (via Loopgate)</div>
          ) : (
            <div className="settings-model-warn">No cloud credential on file. Connect an API key below.</div>
          )}

          <label className="settings-label">Claude model</label>
          <select
            className="settings-input"
            value={anthropicModelChoice}
            onChange={(e) => setAnthropicModelChoice(e.target.value)}
          >
            {ANTHROPIC_MODEL_PRESETS.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
            <option value={CUSTOM_ANTHROPIC}>Custom model id…</option>
          </select>
          {anthropicModelChoice === CUSTOM_ANTHROPIC && (
            <input
              className="settings-input settings-model-input-stack"
              value={anthropicCustomId}
              onChange={(e) => setAnthropicCustomId(e.target.value)}
              placeholder="e.g. claude-sonnet-4-5"
            />
          )}
          <button
            className="retro-btn settings-model-btn-margin"
            type="button"
            disabled={anthropicModelBusy}
            onClick={() => void handleAnthropicModelOnlySave()}
          >
            {anthropicModelBusy ? "Saving…" : "Apply model change"}
          </button>

          <div className="settings-model-subactions">
            {modelSettings.has_cloud_credential && (
              <button className="retro-btn" type="button" onClick={() => setShowReplaceKey((v) => !v)}>
                {showReplaceKey ? "Hide API key form" : "Replace API key…"}
              </button>
            )}
            <button className="retro-btn" type="button" onClick={openLocalFlow}>
              Use local models instead…
            </button>
          </div>

          {modelSettings.has_cloud_credential && showReplaceKey && (
            <div className="settings-model-nested">
              <label className="settings-label">New Anthropic API key</label>
              <input
                className="settings-input"
                type="password"
                autoComplete="off"
                value={replaceKeyValue}
                onChange={(e) => setReplaceKeyValue(e.target.value)}
                placeholder="sk-ant-api…"
              />
              <div className="settings-inline-actions">
                <button
                  className="retro-btn primary"
                  type="button"
                  disabled={anthropicKeyBusy}
                  onClick={() => void handleReplaceAnthropicKey()}
                >
                  {anthropicKeyBusy ? "Saving…" : "Save new key"}
                </button>
                <button className="retro-btn" type="button" onClick={() => { setShowReplaceKey(false); setReplaceKeyValue(""); }}>
                  Cancel
                </button>
              </div>
            </div>
          )}

          {!modelSettings.has_cloud_credential && (
            <div className="settings-model-nested settings-model-warn-panel">
              <div className="settings-label">Connect an API key</div>
              <input
                className="settings-input"
                type="password"
                autoComplete="off"
                value={bootstrapApiKey}
                onChange={(e) => setBootstrapApiKey(e.target.value)}
                placeholder="Anthropic API key (once)"
              />
              <button
                className="retro-btn primary settings-model-btn-margin"
                type="button"
                disabled={anthropicKeyBusy}
                onClick={() => void handleBootstrapAnthropicKey()}
              >
                {anthropicKeyBusy ? "Saving…" : "Save key & enable cloud"}
              </button>
            </div>
          )}

          {anthropicBanner && <div className="settings-model-banner">{anthropicBanner}</div>}
        </>
      )}

      {cloudOpen && (
        <div className="settings-model-flow">
          <div className="settings-model-flow-header">
            <span className="settings-model-flow-title">Connect a cloud model</span>
            <button className="retro-btn" type="button" onClick={cancelFlows}>
              Close
            </button>
          </div>

          {cloudStep === "choose_provider" && (
            <div className="settings-model-flow-body">
              <div className="settings-hint">Step 1 — Pick a provider</div>
              <button
                type="button"
                className="settings-provider-card settings-provider-card-active"
                onClick={() => setCloudStep("connect")}
              >
                <div className="settings-provider-name">Anthropic · Claude</div>
                <div className="settings-provider-desc">Recommended. Keys stay in Keychain; Loopgate calls the API.</div>
              </button>
              <button type="button" className="settings-provider-card settings-provider-card-muted" disabled>
                <div className="settings-provider-name">More providers</div>
                <div className="settings-provider-desc">Coming later.</div>
              </button>
            </div>
          )}

          {cloudStep === "connect" && (
            <div className="settings-model-flow-body">
              <button className="retro-btn settings-model-back" type="button" onClick={() => setCloudStep("choose_provider")}>
                ← Back
              </button>
              <div className="settings-hint">Step 2 — Model &amp; API key</div>
              <label className="settings-label">Claude model</label>
              <select
                className="settings-input"
                value={cloudAnthropicModelChoice}
                onChange={(e) => setCloudAnthropicModelChoice(e.target.value)}
              >
                {ANTHROPIC_MODEL_PRESETS.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
                <option value={CUSTOM_ANTHROPIC}>Custom model id…</option>
              </select>
              {cloudAnthropicModelChoice === CUSTOM_ANTHROPIC && (
                <input
                  className="settings-input settings-model-input-stack"
                  value={cloudAnthropicCustomId}
                  onChange={(e) => setCloudAnthropicCustomId(e.target.value)}
                  placeholder="Model id from Anthropic"
                />
              )}
              <label className="settings-label">Anthropic API key</label>
              <input
                className="settings-input"
                type="password"
                autoComplete="off"
                value={cloudApiKey}
                onChange={(e) => setCloudApiKey(e.target.value)}
                placeholder="Paste once — stored in Keychain via Loopgate"
              />
              <div className="settings-hint">
                Sent only to Loopgate over local IPC. Not written to your repo or settings JSON.
              </div>
              {cloudError && <div className="settings-model-error">{cloudError}</div>}
              <div className="settings-inline-actions">
                <button className="retro-btn primary" type="button" disabled={cloudBusy} onClick={() => void handleCloudConnect()}>
                  {cloudBusy ? "Connecting…" : "Connect & switch"}
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {localOpen && (
        <div className="settings-model-flow">
          <div className="settings-model-flow-header">
            <span className="settings-model-flow-title">Use local models (Ollama)</span>
            <button className="retro-btn" type="button" onClick={cancelFlows}>
              Close
            </button>
          </div>
          <div className="settings-model-flow-body">
            <label className="settings-label">OpenAI-compatible base URL</label>
            <input
              className="settings-input"
              value={localBaseURL}
              onChange={(e) => setLocalBaseURL(e.target.value)}
              placeholder="http://localhost:11434/v1"
            />
            <div className="settings-inline-actions">
              <button className="retro-btn" type="button" disabled={localModelsLoading} onClick={() => void loadLocalModels()}>
                {localModelsLoading ? "Refreshing…" : "Refresh model list"}
              </button>
              <span className="settings-model-detect">
                {ollamaDetected ? "Ollama responded on this Mac." : "Ollama not detected (still try Refresh)."}
              </span>
            </div>
            <label className="settings-label">Model</label>
            {localModels.length > 0 ? (
              <select
                className="settings-input"
                value={localModelPick}
                onChange={(e) => setLocalModelPick(e.target.value)}
              >
                {localModels.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            ) : (
              <div className="settings-hint">No models yet — check the URL and press Refresh.</div>
            )}
            {localError && <div className="settings-model-error">{localError}</div>}
            <div className="settings-inline-actions">
              <button className="retro-btn primary" type="button" disabled={localBusy} onClick={() => void handleLocalSwitch()}>
                {localBusy ? "Switching…" : "Switch to local"}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

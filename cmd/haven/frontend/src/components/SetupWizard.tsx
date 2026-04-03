import { useEffect, useState } from "react";

import { CompleteSetup, DetectOllama, ListLocalModels } from "../../wailsjs/go/main/HavenApp";
import type { SetupRequest } from "../../wailsjs/go/main/HavenApp";
import {
  DEFAULT_WALLPAPER,
  HavenWordmark,
  IconLoopgate,
  IconMorph,
  wallpaperBackgroundImage,
} from "../lib/haven";

type WizardStep = "welcome" | "provider" | "apikey" | "ollama" | "folders" | "ready";

const FOLDER_CHOICES = [
  {
    id: "downloads",
    title: "Downloads",
    description: "Recommended. Lets Morph quietly triage a mirrored copy of Downloads inside Haven.",
    warning: "Good for cleanup prompts and first-run suggestions.",
  },
  {
    id: "desktop",
    title: "Desktop",
    description: "Lets Morph inspect a mirrored copy of your Mac desktop from inside Haven.",
    warning: "Desktop often contains transient or personal items.",
  },
  {
    id: "documents",
    title: "Documents",
    description: "Lets Morph review a mirrored copy of Documents inside Haven.",
    warning: "Documents can contain more sensitive material.",
  },
] as const;

/** First-run wizard: model connection, folder grant, finish. Name, wallpaper, presence, and background live in Settings. */
export default function SetupWizard({ onComplete }: { onComplete: (wallpaperID: string) => void }) {
  const [step, setStep] = useState<WizardStep>("welcome");
  const [provider, setProvider] = useState<"anthropic" | "ollama">("ollama");
  const [apiKey, setAPIKey] = useState("");
  const [ollamaURL, setOllamaURL] = useState("http://localhost:11434/v1");
  const [ollamaDetected, setOllamaDetected] = useState(false);
  const [localModels, setLocalModels] = useState<string[]>([]);
  const [localModelName, setLocalModelName] = useState("");
  const [localModelsLoading, setLocalModelsLoading] = useState(false);
  const [grantedFolderIDs, setGrantedFolderIDs] = useState<string[]>(["downloads"]);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const selectedWallpaper = DEFAULT_WALLPAPER;

  useEffect(() => {
    if (step === "ollama") {
      let cancelled = false;
      setLocalModelsLoading(true);
      Promise.all([
        DetectOllama(),
        ListLocalModels(ollamaURL).catch(() => [] as string[]),
      ]).then(([detected, discoveredModels]) => {
        if (cancelled) return;
        setOllamaDetected(detected);
        setLocalModels(discoveredModels);
        setLocalModelName((previousModelName) => {
          if (discoveredModels.length > 0 && !discoveredModels.includes(previousModelName)) {
            return discoveredModels[0];
          }
          return previousModelName;
        });
      }).finally(() => {
        if (!cancelled) setLocalModelsLoading(false);
      });
      return () => {
        cancelled = true;
      };
    }
  }, [step, ollamaURL]);

  const handleSetup = async () => {
    setSaving(true);
    setError("");
    try {
      const request = {
        provider_name: provider === "anthropic" ? "anthropic" : "openai_compatible",
        model_name: provider === "anthropic" ? "claude-sonnet-4-5" : (localModelName.trim() || localModels[0] || ""),
        api_key: provider === "anthropic" ? apiKey : "",
        base_url: provider === "ollama" ? ollamaURL : "",
        morph_name: "Morph",
        wallpaper: DEFAULT_WALLPAPER.id,
        granted_folder_ids: grantedFolderIDs,
        ambient_enabled: provider === "ollama",
        run_in_background: true,
      } as SetupRequest;
      const response = await CompleteSetup(request);
      if (response.success) {
        localStorage.setItem("haven-wallpaper", DEFAULT_WALLPAPER.id);
        setStep("ready");
      } else {
        setError(response.error || "Setup failed");
      }
    } catch (errorValue) {
      setError(String(errorValue));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="wizard-backdrop" style={{ backgroundImage: wallpaperBackgroundImage(selectedWallpaper) }}>
      <div className="wizard-card">
        {step === "welcome" && (
          <>
            <div className="wizard-brand">
              <HavenWordmark />
            </div>
            <div className="wizard-icon wizard-icon-loopgate"><IconLoopgate /></div>
            <h1 className="wizard-title">Welcome to Haven</h1>
            <p className="wizard-desc">A quiet little operating environment where your assistant can live, work, and leave things for you.</p>
            <div className="wizard-actions">
              <button className="retro-btn primary" onClick={() => setStep("provider")}>Get Started</button>
            </div>
          </>
        )}

        {step === "provider" && (
          <>
            <h2 className="wizard-title">Choose a model</h2>
            <p className="wizard-desc">Local is the calm default. Cloud gives you stronger remote models when you need them.</p>
            <div className="wizard-choices">
              <div className={`wizard-choice ${provider === "ollama" ? "selected" : ""}`} onClick={() => setProvider("ollama")}>
                <div className="wizard-choice-title">Local on this Mac</div>
                <div className="wizard-choice-desc">Recommended. Run with Ollama and keep the model on-device.</div>
              </div>
              <div className={`wizard-choice ${provider === "anthropic" ? "selected" : ""}`} onClick={() => setProvider("anthropic")}>
                <div className="wizard-choice-title">Cloud model</div>
                <div className="wizard-choice-desc">Securely store an API key and use a remote provider.</div>
              </div>
            </div>
            <div className="wizard-actions">
              <button className="retro-btn" onClick={() => setStep("welcome")}>Back</button>
              <button className="retro-btn primary" onClick={() => setStep(provider === "anthropic" ? "apikey" : "ollama")}>Next</button>
            </div>
          </>
        )}

        {step === "apikey" && (
          <>
            <h2 className="wizard-title">Cloud API key</h2>
            <p className="wizard-desc">Loopgate stores this securely in the system keychain and Haven keeps only a non-secret reference.</p>
            <input className="wizard-input" type="password" value={apiKey} onChange={(event) => setAPIKey(event.target.value)} placeholder="sk-ant-..." />
            <div className="wizard-actions">
              <button className="retro-btn" onClick={() => setStep("provider")}>Back</button>
              <button className="retro-btn primary" disabled={!apiKey.trim()} onClick={() => setStep("folders")}>Next</button>
            </div>
          </>
        )}

        {step === "ollama" && (
          <>
            <h2 className="wizard-title">Local model connection</h2>
            {ollamaDetected ? (
              <p className="wizard-desc wizard-success">Ollama is running on this Mac.</p>
            ) : (
              <p className="wizard-desc wizard-warn">Ollama was not detected. Make sure it is running, or point Haven at the local URL below.</p>
            )}
            <input className="wizard-input" value={ollamaURL} onChange={(event) => setOllamaURL(event.target.value)} placeholder="http://localhost:11434/v1" />
            {localModels.length > 0 ? (
              <select className="wizard-input" value={localModelName} onChange={(event) => setLocalModelName(event.target.value)}>
                {localModels.map((modelName) => (
                  <option key={modelName} value={modelName}>{modelName}</option>
                ))}
              </select>
            ) : (
              <input
                className="wizard-input"
                value={localModelName}
                onChange={(event) => setLocalModelName(event.target.value)}
                placeholder={localModelsLoading ? "Discovering local models..." : "Model name (for example llama3.1)"}
              />
            )}
            {localModels.length > 0 && (
              <p className="wizard-desc wizard-success">Using a model discovered at this local endpoint.</p>
            )}
            <div className="wizard-actions">
              <button className="retro-btn" onClick={() => setStep("provider")}>Back</button>
              <button className="retro-btn primary" disabled={!localModels.length && !localModelName.trim()} onClick={() => setStep("folders")}>Next</button>
            </div>
          </>
        )}

        {step === "folders" && (
          <>
            <h2 className="wizard-title">Choose what Morph can see</h2>
            <p className="wizard-desc">Haven always includes “Shared with Morph.” Add mirrored folders so Morph can help without roaming your Mac directly.</p>
            <p className="wizard-desc">Name, wallpaper, presence, and background behavior can be changed anytime in <strong>Settings</strong>.</p>
            <div className="wizard-choices">
              <div className="wizard-choice selected">
                <div className="wizard-choice-title">Shared with Morph</div>
                <div className="wizard-choice-desc">Always on. A deliberate drop zone between your Mac and Haven.</div>
              </div>
              {FOLDER_CHOICES.map((folderChoice) => {
                const selected = grantedFolderIDs.includes(folderChoice.id);
                return (
                  <div
                    key={folderChoice.id}
                    className={`wizard-choice ${selected ? "selected" : ""}`}
                    onClick={() => {
                      setGrantedFolderIDs((previous) => (
                        previous.includes(folderChoice.id)
                          ? previous.filter((folderID) => folderID !== folderChoice.id)
                          : [...previous, folderChoice.id]
                      ));
                    }}
                  >
                    <div className="wizard-choice-title">{folderChoice.title}</div>
                    <div className="wizard-choice-desc">{folderChoice.description}</div>
                    <div className="wizard-choice-desc wizard-warn">{folderChoice.warning}</div>
                  </div>
                );
              })}
            </div>
            {error && <div className="wizard-error">{error}</div>}
            <div className="wizard-actions">
              <button className="retro-btn" onClick={() => setStep(provider === "anthropic" ? "apikey" : "ollama")}>Back</button>
              <button className="retro-btn primary" disabled={saving} onClick={handleSetup}>
                {saving ? "Setting up..." : "Finish setup"}
              </button>
            </div>
          </>
        )}

        {step === "ready" && (
          <>
            <div className="wizard-brand wizard-brand-ready">
              <HavenWordmark />
            </div>
            <div className="wizard-icon"><IconMorph /></div>
            <h2 className="wizard-title">Morph is ready</h2>
            <p className="wizard-desc">Haven OS is set up. Your assistant has a home.</p>
            <div className="wizard-actions">
              <button className="retro-btn primary" onClick={() => onComplete(DEFAULT_WALLPAPER.id)}>Enter Haven</button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

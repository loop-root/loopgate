package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"morph/internal/config"
	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/secrets"
	toolspkg "morph/internal/tools"
)

const (
	havenJournalResidentStateFileName = "haven_journal_resident.json"
	maxHavenJournalResidentBodyBytes  = 4096
	journalResidentMinAttemptGap      = 5 * time.Minute
	journalResidentMinSuccessGap      = 4 * time.Hour
	maxJournalResidentModelRunes      = 8000
)

type havenJournalResidentState struct {
	LastAttemptUTC string `json:"last_attempt_utc,omitempty"`
	LastSuccessUTC string `json:"last_success_utc,omitempty"`
}

// HavenJournalResidentTickResponse is the JSON body for POST /v1/resident/journal-tick.
// The legacy /v1/haven/resident/journal-tick alias returns the same payload.
// Status is one of: wrote, skipped, error. This route is advisory for native clients; it does not
// replace user-initiated journal.write or chat. Loopgate remains sole authority for fs_write.
type HavenJournalResidentTickResponse struct {
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	HavenPath    string `json:"haven_path,omitempty"`
	UpdatedAtUTC string `json:"updated_at_utc,omitempty"`
}

func (server *Server) havenJournalResidentStatePath() string {
	return filepath.Join(server.repoRoot, "runtime", "state", havenJournalResidentStateFileName)
}

func (server *Server) loadHavenJournalResidentState() (havenJournalResidentState, error) {
	path := server.havenJournalResidentStatePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return havenJournalResidentState{}, nil
		}
		return havenJournalResidentState{}, err
	}
	var st havenJournalResidentState
	if err := json.Unmarshal(raw, &st); err != nil {
		return havenJournalResidentState{}, err
	}
	return st, nil
}

func (server *Server) saveHavenJournalResidentState(st havenJournalResidentState) error {
	path := server.havenJournalResidentStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func parseRFC3339OrZero(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func havenDescribeTimeOfDay(t time.Time) string {
	hour := t.Hour()
	switch {
	case hour >= 5 && hour < 9:
		return "early morning"
	case hour >= 9 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 14:
		return "midday"
	case hour >= 14 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 20:
		return "evening"
	case hour >= 20 && hour < 23:
		return "late evening"
	default:
		return "late night"
	}
}

func havenTokenHasCapability(tokenClaims capabilityToken, capabilityName string) bool {
	if len(tokenClaims.AllowedCapabilities) == 0 {
		return true
	}
	_, ok := tokenClaims.AllowedCapabilities[capabilityName]
	return ok
}

func (server *Server) handleHavenJournalResidentTick(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityModelReply) {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(tokenClaims.ActorLabel), "haven") {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "resident journal tick requires actor haven",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxHavenJournalResidentBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	if len(strings.TrimSpace(string(requestBodyBytes))) > 0 {
		var probe struct{}
		if err := decodeJSONBytes(requestBodyBytes, &probe); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
	}

	if !havenTokenHasCapability(tokenClaims, "fs_write") {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "skipped",
			Reason: "capability fs_write not granted on this session",
		})
		return
	}

	state, err := server.loadHavenJournalResidentState()
	if err != nil {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: "resident state unavailable",
		})
		return
	}

	nowWall := server.now()
	nowUTC := nowWall.UTC()
	lastAttempt := parseRFC3339OrZero(state.LastAttemptUTC)
	lastSuccess := parseRFC3339OrZero(state.LastSuccessUTC)

	if !lastAttempt.IsZero() && nowUTC.Sub(lastAttempt) < journalResidentMinAttemptGap {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "skipped",
			Reason: "attempt debounce",
		})
		return
	}
	if !lastSuccess.IsZero() && nowUTC.Sub(lastSuccess) < journalResidentMinSuccessGap {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "skipped",
			Reason: "success cooldown",
		})
		return
	}

	state.LastAttemptUTC = nowUTC.Format(time.RFC3339Nano)
	if err := server.saveHavenJournalResidentState(state); err != nil {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: "cannot persist resident state",
		})
		return
	}

	persona, err := config.LoadPersona(server.repoRoot)
	if err != nil {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: "persona unavailable",
		})
		return
	}

	runtimeConfig, err := modelruntime.LoadConfig(server.repoRoot)
	if err != nil {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: secrets.RedactText(fmt.Sprintf("load model runtime config: %v", err)),
		})
		return
	}

	if isCloudModelConfig(runtimeConfig) {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "skipped",
			Reason: "cloud model active — resident journal disabled to avoid API costs",
		})
		return
	}

	modelClient, _, err := server.newModelClientFromConfig(runtimeConfig)
	if err != nil {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: secrets.RedactText(fmt.Sprintf("initialize model runtime: %v", err)),
		})
		return
	}

	wakeText, err := server.havenWakeStateSummaryText(tokenClaims.TenantID)
	if err != nil {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: "wake-state backend is unavailable",
		})
		return
	}

	timeDesc := havenDescribeTimeOfDay(nowWall)
	prompt := fmt.Sprintf(
		"Write a brief journal entry (2-4 sentences). It's %s on %s. "+
			"Reflect on anything — your environment, a thought, something you're curious about, "+
			"or how you're feeling. Write naturally, as yourself.",
		timeDesc, nowWall.Format("Monday, January 2, 2006"),
	)

	systemFact := "You are writing in your personal journal. Be genuine, introspective, and curious. " +
		"Write as yourself — not as an assistant. No titles, headers, or formatting. Just your thoughts."

	modelCtx, cancelModel := context.WithTimeout(request.Context(), 60*time.Second)
	defer cancelModel()

	modelResponse, modelErr := modelClient.Reply(modelCtx, modelpkg.Request{
		Persona:        persona,
		Policy:         server.policy,
		SessionID:      tokenClaims.ControlSessionID,
		WakeState:      wakeText,
		UserMessage:    prompt,
		RuntimeFacts:   []string{systemFact, modelpkg.HavenConstrainedNativeToolsRuntimeFact},
		Conversation:   nil,
		AvailableTools: nil,
	})
	if modelErr != nil {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: secrets.RedactText(modelErr.Error()),
		})
		return
	}

	text := strings.TrimSpace(modelResponse.AssistantText)
	if text == "" {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: "model returned empty journal text",
		})
		return
	}
	if utf8.RuneCountInString(text) > maxJournalResidentModelRunes {
		runes := []rune(text)
		text = string(runes[:maxJournalResidentModelRunes])
	}

	entryFileName := toolspkg.UniqueJournalEntryFileName(nowWall)
	sandboxPath := fmt.Sprintf("scratch/journal/%s", entryFileName)
	if !strings.HasPrefix(sandboxPath, havenJournalSandboxDir+"/") {
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: "internal journal path validation failed",
		})
		return
	}

	entry := fmt.Sprintf("--- %s ---\n%s\n", nowWall.Local().Format(time.RFC3339Nano), text)
	capResponse := server.executeCapabilityRequest(request.Context(), tokenClaims, CapabilityRequest{
		RequestID:  fmt.Sprintf("journal-resident-%d", nowUTC.UnixNano()),
		Actor:      tokenClaims.ActorLabel,
		SessionID:  tokenClaims.ControlSessionID,
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    sandboxPath,
			"content": entry,
		},
	}, true)

	if capResponse.Status != ResponseStatusSuccess {
		reason := strings.TrimSpace(capResponse.DenialReason)
		if reason == "" {
			reason = string(capResponse.Status)
		}
		server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
			Status: "error",
			Reason: secrets.RedactText(reason),
		})
		return
	}

	state, _ = server.loadHavenJournalResidentState()
	state.LastSuccessUTC = nowUTC.Format(time.RFC3339Nano)
	_ = server.saveHavenJournalResidentState(state)

	havenPath := fmt.Sprintf("research/journal/%s", entryFileName)
	_ = server.logEvent("haven.resident.journal", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id": tokenClaims.ControlSessionID,
		"haven_path":         havenPath,
	})

	server.writeJSON(writer, http.StatusOK, HavenJournalResidentTickResponse{
		Status:       "wrote",
		HavenPath:    havenPath,
		UpdatedAtUTC: nowWall.Local().Format(time.RFC3339Nano),
	})
}

// isCloudModelConfig reports whether the runtime config points to a remote paid API
// (Anthropic, or an OpenAI-compatible endpoint that is not a loopback address).
// Resident background tasks that call the model are skipped for cloud configs to
// avoid unexpected API charges.
func isCloudModelConfig(cfg modelruntime.Config) bool {
	switch cfg.ProviderName {
	case "anthropic":
		return true
	case "openai_compatible":
		return !modelruntime.IsLoopbackModelBaseURL(cfg.BaseURL)
	default:
		return false
	}
}

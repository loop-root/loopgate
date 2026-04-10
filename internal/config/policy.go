package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Policy represents Morph's capability governance configuration.
type Policy struct {
	Version string `yaml:"version" json:"version"`

	Tools struct {
		Filesystem struct {
			ReadEnabled           bool     `yaml:"read_enabled" json:"read_enabled"`
			WriteEnabled          bool     `yaml:"write_enabled" json:"write_enabled"`
			WriteRequiresApproval bool     `yaml:"write_requires_approval" json:"write_requires_approval"`
			AllowedRoots          []string `yaml:"allowed_roots" json:"allowed_roots"`
			DeniedPaths           []string `yaml:"denied_paths" json:"denied_paths"`
		} `yaml:"filesystem" json:"filesystem"`

		HTTP struct {
			Enabled          bool     `yaml:"enabled" json:"enabled"`
			AllowedDomains   []string `yaml:"allowed_domains" json:"allowed_domains"`
			RequiresApproval bool     `yaml:"requires_approval" json:"requires_approval"`
			TimeoutSeconds   int      `yaml:"timeout_seconds" json:"timeout_seconds"`
		} `yaml:"http" json:"http"`

		Shell struct {
			Enabled          bool     `yaml:"enabled" json:"enabled"`
			AllowedCommands  []string `yaml:"allowed_commands" json:"allowed_commands"`
			RequiresApproval bool     `yaml:"requires_approval" json:"requires_approval"`
		} `yaml:"shell" json:"shell"`

		Morphlings struct {
			SpawnEnabled    bool `yaml:"spawn_enabled" json:"spawn_enabled"`
			MaxActive       int  `yaml:"max_active" json:"max_active"`
			RequireTemplate bool `yaml:"require_template" json:"require_template"`
		} `yaml:"morphlings" json:"morphlings"`
	} `yaml:"tools" json:"tools"`

	Logging struct {
		LogCommands         bool `yaml:"log_commands" json:"log_commands"`
		LogMemoryPromotions bool `yaml:"log_memory_promotions" json:"log_memory_promotions"`
		LogToolCalls        bool `yaml:"log_tool_calls" json:"log_tool_calls"`
	} `yaml:"logging" json:"logging"`

	Memory struct {
		AutoDistillate           bool `yaml:"auto_distillate" json:"auto_distillate"`
		RequirePromotionApproval bool `yaml:"require_promotion_approval" json:"require_promotion_approval"`
		ContinuityReviewRequired bool `yaml:"continuity_review_required" json:"continuity_review_required"`
		// AllowRawContinuityInspect keeps the legacy caller-supplied event-bundle route available.
		// Default false: supported clients should prefer server-loaded continuity sources such as
		// /v1/continuity/inspect-thread so raw transcript/event payloads do not become the trust root.
		AllowRawContinuityInspect     bool `yaml:"allow_raw_continuity_inspect" json:"allow_raw_continuity_inspect"`
		SubmitPreviousMinEvents       int  `yaml:"submit_previous_min_events" json:"submit_previous_min_events"`
		SubmitPreviousMinPayloadBytes int  `yaml:"submit_previous_min_payload_bytes" json:"submit_previous_min_payload_bytes"`
		SubmitPreviousMinPromptTokens int  `yaml:"submit_previous_min_prompt_tokens" json:"submit_previous_min_prompt_tokens"`
	} `yaml:"memory" json:"memory"`

	Safety struct {
		AllowPersonaModification bool `yaml:"allow_persona_modification" json:"allow_persona_modification"`
		AllowPolicyModification  bool `yaml:"allow_policy_modification" json:"allow_policy_modification"`
		// HavenTrustedSandboxAutoAllow, when nil, defaults to false (secure default; explicit true required for Haven UX).
		// When true, the control plane may upgrade NeedsApproval to Allow for actor haven + TrustedSandboxLocal tools.
		HavenTrustedSandboxAutoAllow *bool `yaml:"haven_trusted_sandbox_auto_allow" json:"haven_trusted_sandbox_auto_allow"`
		// HavenTrustedSandboxAutoAllowCapabilities, when nil, allows any TrustedSandboxLocal capability name.
		// When non-nil and empty, auto-allow is disabled for all capabilities. When non-nil with entries, only listed names match.
		HavenTrustedSandboxAutoAllowCapabilities *[]string `yaml:"haven_trusted_sandbox_auto_allow_capabilities" json:"haven_trusted_sandbox_auto_allow_capabilities"`
	} `yaml:"safety" json:"safety"`
}

// PolicyLoadResult holds the parsed policy and its content hash.
type PolicyLoadResult struct {
	Policy        Policy
	ContentSHA256 string // hex-encoded SHA-256 of raw policy bytes
}

// LoadPolicy loads policy from core/policy/policy.yaml.
// The repository policy file is required; a missing file is a startup error.
func LoadPolicy(repoRoot string) (Policy, error) {
	result, err := LoadPolicyWithHash(repoRoot)
	if err != nil {
		return Policy{}, err
	}
	return result.Policy, nil
}

// LoadPolicyWithHash loads policy and returns both the parsed policy and
// the SHA-256 hash of the raw policy file contents.
func LoadPolicyWithHash(repoRoot string) (PolicyLoadResult, error) {
	path := filepath.Join(repoRoot, "core", "policy", "policy.yaml")

	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PolicyLoadResult{}, fmt.Errorf("required policy file not found at %s", path)
		}
		return PolicyLoadResult{}, err
	}

	contentHash := sha256.Sum256(rawBytes)
	hashHex := hex.EncodeToString(contentHash[:])

	var pol Policy
	decoder := yaml.NewDecoder(bytes.NewReader(rawBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&pol); err != nil {
		return PolicyLoadResult{}, err
	}

	if err := applyPolicyDefaults(&pol); err != nil {
		return PolicyLoadResult{}, err
	}
	return PolicyLoadResult{Policy: pol, ContentSHA256: hashHex}, nil
}

// VerifyPolicyHash checks the current policy hash against a stored hash file.
// Returns (matched, storedHash, error). If no stored hash exists (first run),
// the current hash is written and (true, "", nil) is returned.
func VerifyPolicyHash(repoRoot string, currentHash string) (bool, string, error) {
	hashPath := filepath.Join(repoRoot, "runtime", "state", "policy_hash.sha256")

	storedBytes, err := os.ReadFile(hashPath)
	if err != nil {
		if os.IsNotExist(err) {
			// First run: write the initial hash silently.
			if mkErr := os.MkdirAll(filepath.Dir(hashPath), 0o700); mkErr != nil {
				return false, "", fmt.Errorf("create policy hash dir: %w", mkErr)
			}
			if wErr := os.WriteFile(hashPath, []byte(currentHash+"\n"), 0o600); wErr != nil {
				return false, "", fmt.Errorf("write initial policy hash: %w", wErr)
			}
			return true, "", nil
		}
		return false, "", fmt.Errorf("read policy hash: %w", err)
	}

	storedHash := strings.TrimSpace(string(storedBytes))
	if storedHash == currentHash {
		return true, storedHash, nil
	}
	return false, storedHash, nil
}

// AcceptPolicyHash writes the current policy hash, replacing any stored value.
func AcceptPolicyHash(repoRoot string, currentHash string) error {
	hashPath := filepath.Join(repoRoot, "runtime", "state", "policy_hash.sha256")
	if err := os.MkdirAll(filepath.Dir(hashPath), 0o700); err != nil {
		return fmt.Errorf("create policy hash dir: %w", err)
	}
	return os.WriteFile(hashPath, []byte(currentHash+"\n"), 0o600)
}

// LoadPolicyFromJSON unmarshals a Policy from JSON bytes, applies defaults, and validates.
func LoadPolicyFromJSON(data []byte) (Policy, error) {
	var pol Policy
	if err := json.Unmarshal(data, &pol); err != nil {
		return Policy{}, fmt.Errorf("decode policy JSON: %w", err)
	}
	if err := applyPolicyDefaults(&pol); err != nil {
		return Policy{}, err
	}
	return pol, nil
}

// PolicyToJSON marshals a Policy to indented JSON bytes.
func PolicyToJSON(p Policy) ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

func applyPolicyDefaults(pol *Policy) error {
	if pol.Version == "" {
		pol.Version = "0.1.0"
	}
	pol.Tools.Filesystem.AllowedRoots = normalizeConfiguredPaths(pol.Tools.Filesystem.AllowedRoots)
	if (pol.Tools.Filesystem.ReadEnabled || pol.Tools.Filesystem.WriteEnabled) && len(pol.Tools.Filesystem.AllowedRoots) == 0 {
		return fmt.Errorf("filesystem allowed_roots must be explicitly configured when filesystem access is enabled")
	}
	pol.Tools.Filesystem.DeniedPaths = normalizeConfiguredPaths(pol.Tools.Filesystem.DeniedPaths)
	if pol.Tools.HTTP.TimeoutSeconds <= 0 {
		pol.Tools.HTTP.TimeoutSeconds = 10
	}
	if pol.Tools.Morphlings.MaxActive <= 0 {
		pol.Tools.Morphlings.MaxActive = 5
	}
	if pol.Memory.SubmitPreviousMinEvents <= 0 {
		pol.Memory.SubmitPreviousMinEvents = 3
	}
	if pol.Memory.SubmitPreviousMinPayloadBytes <= 0 {
		pol.Memory.SubmitPreviousMinPayloadBytes = 512
	}
	if pol.Memory.SubmitPreviousMinPromptTokens <= 0 {
		pol.Memory.SubmitPreviousMinPromptTokens = 120
	}
	return nil
}

// HavenTrustedSandboxAutoAllowEnabled reports whether Haven may auto-allow TrustedSandboxLocal capabilities
// that would otherwise require approval. Omitted policy field defaults to false.
func (p Policy) HavenTrustedSandboxAutoAllowEnabled() bool {
	if p.Safety.HavenTrustedSandboxAutoAllow == nil {
		return false
	}
	return *p.Safety.HavenTrustedSandboxAutoAllow
}

// HavenTrustedSandboxAutoAllowMatchesCapability reports whether capabilityName is permitted for
// Haven trusted-sandbox auto-allow. Omitted allowlist means all capabilities; a non-nil empty
// slice matches nothing; a non-nil non-empty slice is an explicit allowlist.
func (p Policy) HavenTrustedSandboxAutoAllowMatchesCapability(capabilityName string) bool {
	listPtr := p.Safety.HavenTrustedSandboxAutoAllowCapabilities
	if listPtr == nil {
		return true
	}
	list := *listPtr
	if len(list) == 0 {
		return false
	}
	return slices.Contains(list, capabilityName)
}

func normalizeConfiguredPaths(rawPaths []string) []string {
	normalized := make([]string, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, expandHomePrefix(trimmed))
	}
	return normalized
}

func expandHomePrefix(pathValue string) string {
	if pathValue == "~" {
		if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
			return homeDir
		}
		return pathValue
	}

	prefix := "~" + string(os.PathSeparator)
	if strings.HasPrefix(pathValue, prefix) {
		if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
			return filepath.Join(homeDir, pathValue[len(prefix):])
		}
	}
	return pathValue
}

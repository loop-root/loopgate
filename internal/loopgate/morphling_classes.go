package loopgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"morph/internal/config"
	"morph/internal/identifiers"
	toolspkg "morph/internal/tools"
)

const morphlingClassPolicyRelativePath = "core/policy/morphling_classes.yaml"

var knownMorphlingSandboxZones = map[string]struct{}{
	"agents":     {},
	"imports":    {},
	"logs":       {},
	"outputs":    {},
	"quarantine": {},
	"scratch":    {},
	"tmp":        {},
	"workspace":  {},
}

type morphlingClassPolicy struct {
	Version string
	Classes map[string]validatedMorphlingClass
}

type validatedMorphlingClass struct {
	Name                     string
	Description              string
	AllowedCapabilities      []string
	AllowedZones             []string
	MaxConcurrent            int
	MaxDiskBytes             int64
	MaxTimeSeconds           int
	MaxTokens                int
	SpawnApprovalTTLSeconds  int
	CapabilityTokenTTLSeconds int
	ReviewTTLSeconds         int
	SpawnRequiresApproval    bool
	CompletionRequiresReview bool
}

type morphlingClassPolicyFile struct {
	Version string                  `yaml:"version" json:"version"`
	Classes []morphlingClassYAMLDef `yaml:"classes" json:"classes"`
}

type morphlingClassYAMLDef struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Capabilities struct {
		Allowed []string `yaml:"allowed" json:"allowed"`
	} `yaml:"capabilities" json:"capabilities"`
	Sandbox struct {
		AllowedZones []string `yaml:"allowed_zones" json:"allowed_zones"`
	} `yaml:"sandbox" json:"sandbox"`
	ResourceLimits struct {
		MaxTimeSeconds int   `yaml:"max_time_seconds" json:"max_time_seconds"`
		MaxTokens      int   `yaml:"max_tokens" json:"max_tokens"`
		MaxDiskBytes   int64 `yaml:"max_disk_bytes" json:"max_disk_bytes"`
	} `yaml:"resource_limits" json:"resource_limits"`
	TTL struct {
		SpawnApprovalTTLSeconds   int `yaml:"spawn_approval_ttl_seconds" json:"spawn_approval_ttl_seconds"`
		CapabilityTokenTTLSeconds int `yaml:"capability_token_ttl_seconds" json:"capability_token_ttl_seconds"`
		ReviewTTLSeconds          int `yaml:"review_ttl_seconds" json:"review_ttl_seconds"`
	} `yaml:"ttl" json:"ttl"`
	SpawnRequiresApproval    bool `yaml:"spawn_requires_approval" json:"spawn_requires_approval"`
	CompletionRequiresReview bool `yaml:"completion_requires_review" json:"completion_requires_review"`
	MaxConcurrent            int  `yaml:"max_concurrent" json:"max_concurrent"`
}

func loadMorphlingClassPolicyWithSeed(configStateDir, repoRoot string, registry *toolspkg.Registry) (morphlingClassPolicy, error) {
	// Try JSON state first.
	jsonResult, err := config.LoadJSONConfig[morphlingClassPolicyFile](configStateDir, "morphling_classes")
	if err == nil {
		data, marshalErr := json.Marshal(jsonResult)
		if marshalErr != nil {
			return morphlingClassPolicy{}, fmt.Errorf("re-marshal morphling class policy: %w", marshalErr)
		}
		return loadMorphlingClassPolicyFromJSON(data, registry)
	}
	if !os.IsNotExist(err) {
		return morphlingClassPolicy{}, fmt.Errorf("load morphling class policy JSON state: %w", err)
	}

	// Fall back to YAML seed.
	result, yamlErr := loadMorphlingClassPolicy(repoRoot, registry)
	if yamlErr != nil {
		return morphlingClassPolicy{}, yamlErr
	}

	// Build the file representation for JSON persistence.
	classList := make([]morphlingClassYAMLDef, 0, len(result.Classes))
	for _, cls := range result.Classes {
		classList = append(classList, morphlingClassYAMLDef{
			Name:        cls.Name,
			Description: cls.Description,
			Capabilities: struct {
				Allowed []string `yaml:"allowed" json:"allowed"`
			}{Allowed: cls.AllowedCapabilities},
			Sandbox: struct {
				AllowedZones []string `yaml:"allowed_zones" json:"allowed_zones"`
			}{AllowedZones: cls.AllowedZones},
			ResourceLimits: struct {
				MaxTimeSeconds int   `yaml:"max_time_seconds" json:"max_time_seconds"`
				MaxTokens      int   `yaml:"max_tokens" json:"max_tokens"`
				MaxDiskBytes   int64 `yaml:"max_disk_bytes" json:"max_disk_bytes"`
			}{MaxTimeSeconds: cls.MaxTimeSeconds, MaxTokens: cls.MaxTokens, MaxDiskBytes: cls.MaxDiskBytes},
			TTL: struct {
				SpawnApprovalTTLSeconds   int `yaml:"spawn_approval_ttl_seconds" json:"spawn_approval_ttl_seconds"`
				CapabilityTokenTTLSeconds int `yaml:"capability_token_ttl_seconds" json:"capability_token_ttl_seconds"`
				ReviewTTLSeconds          int `yaml:"review_ttl_seconds" json:"review_ttl_seconds"`
			}{SpawnApprovalTTLSeconds: cls.SpawnApprovalTTLSeconds, CapabilityTokenTTLSeconds: cls.CapabilityTokenTTLSeconds, ReviewTTLSeconds: cls.ReviewTTLSeconds},
			SpawnRequiresApproval:    cls.SpawnRequiresApproval,
			CompletionRequiresReview: cls.CompletionRequiresReview,
			MaxConcurrent:            cls.MaxConcurrent,
		})
	}
	fileData := morphlingClassPolicyFile{Version: result.Version, Classes: classList}
	if saveErr := config.SaveJSONConfig(configStateDir, "morphling_classes", fileData); saveErr != nil {
		return result, fmt.Errorf("seed morphling class policy JSON: %w", saveErr)
	}

	return result, nil
}

func loadMorphlingClassPolicy(repoRoot string, registry *toolspkg.Registry) (morphlingClassPolicy, error) {
	policyPath := filepath.Join(repoRoot, morphlingClassPolicyRelativePath)
	rawPolicyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return morphlingClassPolicy{}, fmt.Errorf("required morphling class policy file not found at %s", policyPath)
		}
		return morphlingClassPolicy{}, fmt.Errorf("read morphling class policy: %w", err)
	}

	var rawPolicy morphlingClassPolicyFile
	decoder := yaml.NewDecoder(bytes.NewReader(rawPolicyBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&rawPolicy); err != nil {
		return morphlingClassPolicy{}, fmt.Errorf("decode morphling class policy: %w", err)
	}

	validatedPolicy := morphlingClassPolicy{
		Version: strings.TrimSpace(rawPolicy.Version),
		Classes: make(map[string]validatedMorphlingClass, len(rawPolicy.Classes)),
	}
	if validatedPolicy.Version == "" {
		validatedPolicy.Version = "1"
	}
	if len(rawPolicy.Classes) == 0 {
		return morphlingClassPolicy{}, fmt.Errorf("morphling class policy must define at least one class")
	}

	for _, rawClass := range rawPolicy.Classes {
		validatedClass, err := validateMorphlingClassDefinition(rawClass, registry)
		if err != nil {
			return morphlingClassPolicy{}, fmt.Errorf("validate morphling class %q: %w", strings.TrimSpace(rawClass.Name), err)
		}
		if _, exists := validatedPolicy.Classes[validatedClass.Name]; exists {
			return morphlingClassPolicy{}, fmt.Errorf("duplicate morphling class %q", validatedClass.Name)
		}
		validatedPolicy.Classes[validatedClass.Name] = validatedClass
	}

	return validatedPolicy, nil
}

func loadMorphlingClassPolicyFromJSON(data []byte, registry *toolspkg.Registry) (morphlingClassPolicy, error) {
	var rawPolicy morphlingClassPolicyFile
	if err := json.Unmarshal(data, &rawPolicy); err != nil {
		return morphlingClassPolicy{}, fmt.Errorf("decode morphling class policy JSON: %w", err)
	}

	validatedPolicy := morphlingClassPolicy{
		Version: strings.TrimSpace(rawPolicy.Version),
		Classes: make(map[string]validatedMorphlingClass, len(rawPolicy.Classes)),
	}
	if validatedPolicy.Version == "" {
		validatedPolicy.Version = "1"
	}
	if len(rawPolicy.Classes) == 0 {
		return morphlingClassPolicy{}, fmt.Errorf("morphling class policy must define at least one class")
	}

	for _, rawClass := range rawPolicy.Classes {
		validatedClass, err := validateMorphlingClassDefinition(rawClass, registry)
		if err != nil {
			return morphlingClassPolicy{}, fmt.Errorf("validate morphling class %q: %w", strings.TrimSpace(rawClass.Name), err)
		}
		if _, exists := validatedPolicy.Classes[validatedClass.Name]; exists {
			return morphlingClassPolicy{}, fmt.Errorf("duplicate morphling class %q", validatedClass.Name)
		}
		validatedPolicy.Classes[validatedClass.Name] = validatedClass
	}

	return validatedPolicy, nil
}

func validateMorphlingClassDefinition(rawClass morphlingClassYAMLDef, registry *toolspkg.Registry) (validatedMorphlingClass, error) {
	className := strings.TrimSpace(rawClass.Name)
	if err := identifiers.ValidateSafeIdentifier("morphling class", className); err != nil {
		return validatedMorphlingClass{}, err
	}
	description := strings.TrimSpace(rawClass.Description)
	if description == "" {
		return validatedMorphlingClass{}, fmt.Errorf("description is required")
	}

	allowedCapabilities, err := normalizeMorphlingCapabilityList(rawClass.Capabilities.Allowed, registry)
	if err != nil {
		return validatedMorphlingClass{}, err
	}
	allowedZones, err := normalizeMorphlingZoneList(rawClass.Sandbox.AllowedZones)
	if err != nil {
		return validatedMorphlingClass{}, err
	}
	if rawClass.ResourceLimits.MaxTimeSeconds <= 0 {
		return validatedMorphlingClass{}, fmt.Errorf("resource_limits.max_time_seconds must be positive")
	}
	if rawClass.ResourceLimits.MaxTokens <= 0 {
		return validatedMorphlingClass{}, fmt.Errorf("resource_limits.max_tokens must be positive")
	}
	if rawClass.ResourceLimits.MaxDiskBytes <= 0 {
		return validatedMorphlingClass{}, fmt.Errorf("resource_limits.max_disk_bytes must be positive")
	}
	if rawClass.TTL.SpawnApprovalTTLSeconds <= 0 {
		return validatedMorphlingClass{}, fmt.Errorf("ttl.spawn_approval_ttl_seconds must be positive")
	}
	if rawClass.TTL.CapabilityTokenTTLSeconds <= 0 {
		return validatedMorphlingClass{}, fmt.Errorf("ttl.capability_token_ttl_seconds must be positive")
	}
	if rawClass.TTL.ReviewTTLSeconds <= 0 {
		return validatedMorphlingClass{}, fmt.Errorf("ttl.review_ttl_seconds must be positive")
	}
	if rawClass.MaxConcurrent <= 0 {
		return validatedMorphlingClass{}, fmt.Errorf("max_concurrent must be positive")
	}
	if rawClass.TTL.CapabilityTokenTTLSeconds < rawClass.ResourceLimits.MaxTimeSeconds {
		return validatedMorphlingClass{}, fmt.Errorf("ttl.capability_token_ttl_seconds must be at least resource_limits.max_time_seconds")
	}

	return validatedMorphlingClass{
		Name:                      className,
		Description:               description,
		AllowedCapabilities:       allowedCapabilities,
		AllowedZones:              allowedZones,
		MaxConcurrent:             rawClass.MaxConcurrent,
		MaxDiskBytes:              rawClass.ResourceLimits.MaxDiskBytes,
		MaxTimeSeconds:            rawClass.ResourceLimits.MaxTimeSeconds,
		MaxTokens:                 rawClass.ResourceLimits.MaxTokens,
		SpawnApprovalTTLSeconds:   rawClass.TTL.SpawnApprovalTTLSeconds,
		CapabilityTokenTTLSeconds: rawClass.TTL.CapabilityTokenTTLSeconds,
		ReviewTTLSeconds:          rawClass.TTL.ReviewTTLSeconds,
		SpawnRequiresApproval:     rawClass.SpawnRequiresApproval,
		CompletionRequiresReview:  rawClass.CompletionRequiresReview,
	}, nil
}

func normalizeMorphlingCapabilityList(rawCapabilities []string, registry *toolspkg.Registry) ([]string, error) {
	if len(rawCapabilities) == 0 {
		return nil, fmt.Errorf("capabilities.allowed must include at least one capability")
	}
	seenCapabilities := make(map[string]struct{}, len(rawCapabilities))
	validatedCapabilities := make([]string, 0, len(rawCapabilities))
	for _, rawCapabilityName := range rawCapabilities {
		capabilityName := strings.TrimSpace(rawCapabilityName)
		if err := identifiers.ValidateSafeIdentifier("morphling capability", capabilityName); err != nil {
			return nil, err
		}
		if registry.Get(capabilityName) == nil {
			return nil, fmt.Errorf("unknown capability %q", capabilityName)
		}
		if _, exists := seenCapabilities[capabilityName]; exists {
			return nil, fmt.Errorf("duplicate capability %q", capabilityName)
		}
		seenCapabilities[capabilityName] = struct{}{}
		validatedCapabilities = append(validatedCapabilities, capabilityName)
	}
	sort.Strings(validatedCapabilities)
	return validatedCapabilities, nil
}

func normalizeMorphlingZoneList(rawZones []string) ([]string, error) {
	if len(rawZones) == 0 {
		return nil, fmt.Errorf("sandbox.allowed_zones must include at least one zone")
	}
	seenZones := make(map[string]struct{}, len(rawZones))
	validatedZones := make([]string, 0, len(rawZones))
	for _, rawZoneName := range rawZones {
		zoneName := strings.TrimSpace(rawZoneName)
		if err := identifiers.ValidateSafeIdentifier("morphling sandbox zone", zoneName); err != nil {
			return nil, err
		}
		if _, known := knownMorphlingSandboxZones[zoneName]; !known {
			return nil, fmt.Errorf("unknown sandbox zone %q", zoneName)
		}
		if _, exists := seenZones[zoneName]; exists {
			return nil, fmt.Errorf("duplicate sandbox zone %q", zoneName)
		}
		seenZones[zoneName] = struct{}{}
		validatedZones = append(validatedZones, zoneName)
	}
	sort.Strings(validatedZones)
	return validatedZones, nil
}

func (policy morphlingClassPolicy) Class(className string) (validatedMorphlingClass, bool) {
	validatedClass, found := policy.Classes[strings.TrimSpace(className)]
	return validatedClass, found
}

func (validatedClass validatedMorphlingClass) AllowsZone(zoneName string) bool {
	return slices.Contains(validatedClass.AllowedZones, strings.TrimSpace(zoneName))
}

func morphlingZoneForRelativePath(relativePath string) string {
	cleanedPath := strings.TrimSpace(relativePath)
	if cleanedPath == "" || cleanedPath == "." {
		return ""
	}
	pathParts := strings.Split(cleanedPath, "/")
	if len(pathParts) == 0 {
		return ""
	}
	return pathParts[0]
}

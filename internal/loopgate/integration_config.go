package loopgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"mime"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"loopgate/internal/config"
	"loopgate/internal/identifiers"
	"loopgate/internal/secrets"
)

const (
	connectionConfigDir               = "loopgate/connections"
	maxResponseBodyBytes              = 1 << 20
	maxTokenBodyBytes                 = 64 << 10
	maxConfiguredListItems            = 10
	contentClassStructuredJSONConfig  = "structured_json"
	contentClassMarkdownConfig        = "markdown"
	contentClassHTMLConfig            = "html"
	extractorJSONFieldAllowlistConfig = "json_field_allowlist"
	extractorJSONNestedSelectorConfig = "json_nested_selector"
	extractorJSONObjectListSelector   = "json_object_list_selector"
	extractorMarkdownFrontmatterKeys  = "markdown_frontmatter_keys"
	extractorMarkdownSectionSelector  = "markdown_section_selector"
	extractorHTMLMetaAllowlistConfig  = "html_meta_allowlist"
)

type connectionConfigFile struct {
	Provider         string                       `yaml:"provider" json:"provider"`
	GrantType        string                       `yaml:"grant_type" json:"grant_type"`
	Subject          string                       `yaml:"subject" json:"subject"`
	ClientID         string                       `yaml:"client_id" json:"client_id"`
	AuthorizationURL string                       `yaml:"authorization_url" json:"authorization_url"`
	TokenURL         string                       `yaml:"token_url" json:"token_url"`
	RedirectURL      string                       `yaml:"redirect_url" json:"redirect_url"`
	APIBaseURL       string                       `yaml:"api_base_url" json:"api_base_url"`
	AllowedHosts     []string                     `yaml:"allowed_hosts" json:"allowed_hosts"`
	Scopes           []string                     `yaml:"scopes" json:"scopes"`
	Credential       secrets.SecretRef            `yaml:"credential" json:"credential"`
	Capabilities     []connectionCapabilityConfig `yaml:"capabilities" json:"capabilities"`
}

type connectionCapabilityConfig struct {
	Name           string                            `yaml:"name" json:"name"`
	Description    string                            `yaml:"description" json:"description"`
	Method         string                            `yaml:"method" json:"method"`
	Path           string                            `yaml:"path" json:"path"`
	ContentClass   string                            `yaml:"content_class" json:"content_class"`
	Extractor      string                            `yaml:"extractor" json:"extractor"`
	ResponseFields []connectionCapabilityFieldConfig `yaml:"response_fields" json:"response_fields"`
}

type connectionCapabilityFieldConfig struct {
	Name                 string   `yaml:"name" json:"name"`
	JSONField            string   `yaml:"json_field" json:"json_field"`
	JSONPath             string   `yaml:"json_path" json:"json_path"`
	JSONListItemFields   []string `yaml:"json_list_item_fields" json:"json_list_item_fields"`
	MaxItems             int      `yaml:"max_items" json:"max_items"`
	FrontmatterKey       string   `yaml:"frontmatter_key" json:"frontmatter_key"`
	HeadingPath          []string `yaml:"heading_path" json:"heading_path"`
	HTMLTitle            bool     `yaml:"html_title" json:"html_title"`
	MetaName             string   `yaml:"meta_name" json:"meta_name"`
	MetaProperty         string   `yaml:"meta_property" json:"meta_property"`
	Sensitivity          string   `yaml:"sensitivity" json:"sensitivity"`
	MaxInlineBytes       int      `yaml:"max_inline_bytes" json:"max_inline_bytes"`
	AllowBlobRefFallback bool     `yaml:"allow_blob_ref_fallback" json:"allow_blob_ref_fallback"`
	AllowSuspiciousName  bool     `yaml:"allow_suspicious_name" json:"allow_suspicious_name"`
}

type configuredConnection struct {
	Registration     connectionRegistration
	ClientID         string
	AuthorizationURL *url.URL
	TokenURL         *url.URL
	RedirectURL      string
	APIBaseURL       *url.URL
	AllowedHosts     map[string]struct{}
}

type configuredCapability struct {
	Name           string
	Description    string
	Method         string
	Path           string
	ContentClass   string
	Extractor      string
	ResponseFields []configuredCapabilityField
	ConnectionKey  string
}

type configuredCapabilityField struct {
	Name                 string
	JSONField            string
	JSONPath             string
	JSONListItemFields   []string
	MaxItems             int
	FrontmatterKey       string
	HeadingPath          []string
	HTMLTitle            bool
	MetaName             string
	MetaProperty         string
	Sensitivity          string
	MaxInlineBytes       int
	AllowBlobRefFallback bool
	AllowSuspiciousName  bool
}

func loadConfiguredConnectionsWithSeed(configStateDir, repoRoot string) (map[string]configuredConnection, map[string]configuredCapability, error) {
	// Try JSON state first.
	jsonConfigs, err := config.LoadJSONConfig[[]connectionConfigFile](configStateDir, "connections")
	if err == nil {
		data, marshalErr := json.Marshal(jsonConfigs)
		if marshalErr != nil {
			return nil, nil, fmt.Errorf("re-marshal connections: %w", marshalErr)
		}
		return loadConfiguredConnectionsFromJSON(data)
	}
	if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("load connections JSON state: %w", err)
	}

	// Fall back to YAML seed.
	connections, capabilities, yamlErr := loadConfiguredConnections(repoRoot)
	if yamlErr != nil {
		return nil, nil, yamlErr
	}

	// Build array of connectionConfigFile for JSON persistence.
	// Re-read the YAML files to get the raw config representation.
	configDir := filepath.Join(repoRoot, connectionConfigDir)
	dirEntries, readErr := os.ReadDir(configDir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			// No YAML files, persist empty array.
			if saveErr := config.SaveJSONConfig(configStateDir, "connections", []connectionConfigFile{}); saveErr != nil {
				return connections, capabilities, fmt.Errorf("seed connections JSON: %w", saveErr)
			}
			return connections, capabilities, nil
		}
		return connections, capabilities, nil // Already loaded from YAML, just skip persistence.
	}

	var configFiles []connectionConfigFile
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			continue
		}
		name := dirEntry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		configPath := filepath.Join(configDir, name)
		rawBytes, readFileErr := os.ReadFile(configPath)
		if readFileErr != nil {
			continue
		}
		var parsed connectionConfigFile
		decoder := yaml.NewDecoder(bytes.NewReader(rawBytes))
		decoder.KnownFields(true)
		if decodeErr := decoder.Decode(&parsed); decodeErr != nil {
			continue
		}
		configFiles = append(configFiles, parsed)
	}
	if configFiles == nil {
		configFiles = []connectionConfigFile{}
	}
	if saveErr := config.SaveJSONConfig(configStateDir, "connections", configFiles); saveErr != nil {
		return connections, capabilities, fmt.Errorf("seed connections JSON: %w", saveErr)
	}

	return connections, capabilities, nil
}

func loadConfiguredConnections(repoRoot string) (map[string]configuredConnection, map[string]configuredCapability, error) {
	configDir := filepath.Join(repoRoot, connectionConfigDir)
	dirEntries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]configuredConnection{}, map[string]configuredCapability{}, nil
		}
		return nil, nil, fmt.Errorf("read connection config dir: %w", err)
	}

	connectionDefinitions := make(map[string]configuredConnection)
	capabilityDefinitions := make(map[string]configuredCapability)
	fileNames := make([]string, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			continue
		}
		name := dirEntry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			fileNames = append(fileNames, name)
		}
	}
	sort.Strings(fileNames)

	for _, fileName := range fileNames {
		configPath := filepath.Join(configDir, fileName)
		loadedConnection, loadedCapabilities, err := loadConfiguredConnectionFile(configPath)
		if err != nil {
			return nil, nil, err
		}
		connectionKey := connectionRecordKey(loadedConnection.Registration.Provider, loadedConnection.Registration.Subject)
		if _, exists := connectionDefinitions[connectionKey]; exists {
			return nil, nil, fmt.Errorf("duplicate configured connection for provider %q subject %q", loadedConnection.Registration.Provider, loadedConnection.Registration.Subject)
		}
		connectionDefinitions[connectionKey] = loadedConnection
		for capabilityName, capabilityDefinition := range loadedCapabilities {
			if _, exists := capabilityDefinitions[capabilityName]; exists {
				return nil, nil, fmt.Errorf("duplicate configured capability %q", capabilityName)
			}
			capabilityDefinitions[capabilityName] = capabilityDefinition
		}
	}

	return connectionDefinitions, capabilityDefinitions, nil
}

func loadConfiguredConnectionFile(configPath string) (configuredConnection, map[string]configuredCapability, error) {
	rawConfigBytes, err := os.ReadFile(configPath)
	if err != nil {
		return configuredConnection{}, nil, fmt.Errorf("read connection config %s: %w", configPath, err)
	}

	var parsedConfig connectionConfigFile
	decoder := yaml.NewDecoder(bytes.NewReader(rawConfigBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&parsedConfig); err != nil {
		return configuredConnection{}, nil, fmt.Errorf("decode connection config %s: %w", configPath, err)
	}

	normalizedConfig, err := normalizeConnectionConfig(parsedConfig)
	if err != nil {
		return configuredConnection{}, nil, fmt.Errorf("validate connection config %s: %w", configPath, err)
	}

	connectionKey := connectionRecordKey(normalizedConfig.Provider, normalizedConfig.Subject)
	capabilityDefinitions := make(map[string]configuredCapability, len(normalizedConfig.Capabilities))
	for _, configuredCapabilityDef := range normalizedConfig.Capabilities {
		if _, exists := capabilityDefinitions[configuredCapabilityDef.Name]; exists {
			return configuredConnection{}, nil, fmt.Errorf("duplicate capability %q in %s", configuredCapabilityDef.Name, configPath)
		}
		configuredCapabilityFields := make([]configuredCapabilityField, 0, len(configuredCapabilityDef.ResponseFields))
		for _, configuredFieldConfig := range configuredCapabilityDef.ResponseFields {
			configuredCapabilityFields = append(configuredCapabilityFields, configuredCapabilityField{
				Name:                 configuredFieldConfig.Name,
				JSONField:            configuredFieldConfig.JSONField,
				JSONPath:             configuredFieldConfig.JSONPath,
				JSONListItemFields:   append([]string(nil), configuredFieldConfig.JSONListItemFields...),
				MaxItems:             configuredFieldConfig.MaxItems,
				FrontmatterKey:       configuredFieldConfig.FrontmatterKey,
				HeadingPath:          append([]string(nil), configuredFieldConfig.HeadingPath...),
				HTMLTitle:            configuredFieldConfig.HTMLTitle,
				MetaName:             configuredFieldConfig.MetaName,
				MetaProperty:         configuredFieldConfig.MetaProperty,
				Sensitivity:          configuredFieldConfig.Sensitivity,
				MaxInlineBytes:       configuredFieldConfig.MaxInlineBytes,
				AllowBlobRefFallback: configuredFieldConfig.AllowBlobRefFallback,
				AllowSuspiciousName:  configuredFieldConfig.AllowSuspiciousName,
			})
		}
		capabilityDefinitions[configuredCapabilityDef.Name] = configuredCapability{
			Name:           configuredCapabilityDef.Name,
			Description:    configuredCapabilityDef.Description,
			Method:         configuredCapabilityDef.Method,
			Path:           configuredCapabilityDef.Path,
			ContentClass:   configuredCapabilityDef.ContentClass,
			Extractor:      configuredCapabilityDef.Extractor,
			ResponseFields: configuredCapabilityFields,
			ConnectionKey:  connectionKey,
		}
	}

	allowedHosts := make(map[string]struct{}, len(normalizedConfig.AllowedHosts))
	for _, allowedHost := range normalizedConfig.AllowedHosts {
		allowedHosts[allowedHost] = struct{}{}
	}

	var authorizationURL *url.URL
	if normalizedConfig.GrantType == controlapipkg.GrantTypePKCE {
		authorizationURL, err = parseAndValidateConfiguredURL("authorization_url", normalizedConfig.AuthorizationURL, allowedHosts)
		if err != nil {
			return configuredConnection{}, nil, fmt.Errorf("%s: %w", configPath, err)
		}
	}
	var tokenURL *url.URL
	if normalizedConfig.GrantType != controlapipkg.GrantTypePublicRead {
		tokenURL, err = parseAndValidateConfiguredURL("token_url", normalizedConfig.TokenURL, allowedHosts)
		if err != nil {
			return configuredConnection{}, nil, fmt.Errorf("%s: %w", configPath, err)
		}
	}
	apiBaseURL, err := parseAndValidateConfiguredURL("api_base_url", normalizedConfig.APIBaseURL, allowedHosts)
	if err != nil {
		return configuredConnection{}, nil, fmt.Errorf("%s: %w", configPath, err)
	}

	return configuredConnection{
		Registration: connectionRegistration{
			Provider:   normalizedConfig.Provider,
			GrantType:  normalizedConfig.GrantType,
			Subject:    normalizedConfig.Subject,
			Scopes:     append([]string(nil), normalizedConfig.Scopes...),
			Credential: normalizedConfig.Credential,
		},
		ClientID:         normalizedConfig.ClientID,
		AuthorizationURL: authorizationURL,
		TokenURL:         tokenURL,
		RedirectURL:      normalizedConfig.RedirectURL,
		APIBaseURL:       apiBaseURL,
		AllowedHosts:     allowedHosts,
	}, capabilityDefinitions, nil
}

// loadConfiguredConnectionsFromJSON consolidates multiple YAML connection files
// into a single JSON array. JSON state stores all connections as one array of connectionConfigFile.
func loadConfiguredConnectionsFromJSON(data []byte) (map[string]configuredConnection, map[string]configuredCapability, error) {
	var configs []connectionConfigFile
	if err := decodeJSONBytes(data, &configs); err != nil {
		return nil, nil, fmt.Errorf("decode connections JSON: %w", err)
	}
	return loadConfiguredConnectionsFromConfigFiles(configs)
}

func loadConfiguredConnectionsFromConfigFiles(configs []connectionConfigFile) (map[string]configuredConnection, map[string]configuredCapability, error) {
	connectionDefinitions := make(map[string]configuredConnection)
	capabilityDefinitions := make(map[string]configuredCapability)

	for _, rawConfig := range configs {
		normalizedConfig, err := normalizeConnectionConfig(rawConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("validate connection %q/%q: %w", rawConfig.Provider, rawConfig.Subject, err)
		}

		connectionKey := connectionRecordKey(normalizedConfig.Provider, normalizedConfig.Subject)
		if _, exists := connectionDefinitions[connectionKey]; exists {
			return nil, nil, fmt.Errorf("duplicate configured connection for provider %q subject %q", normalizedConfig.Provider, normalizedConfig.Subject)
		}

		caps := make(map[string]configuredCapability, len(normalizedConfig.Capabilities))
		for _, configuredCapabilityDef := range normalizedConfig.Capabilities {
			if _, exists := caps[configuredCapabilityDef.Name]; exists {
				return nil, nil, fmt.Errorf("duplicate capability %q", configuredCapabilityDef.Name)
			}
			configuredCapabilityFields := make([]configuredCapabilityField, 0, len(configuredCapabilityDef.ResponseFields))
			for _, configuredFieldConfig := range configuredCapabilityDef.ResponseFields {
				configuredCapabilityFields = append(configuredCapabilityFields, configuredCapabilityField{
					Name:                 configuredFieldConfig.Name,
					JSONField:            configuredFieldConfig.JSONField,
					JSONPath:             configuredFieldConfig.JSONPath,
					JSONListItemFields:   append([]string(nil), configuredFieldConfig.JSONListItemFields...),
					MaxItems:             configuredFieldConfig.MaxItems,
					FrontmatterKey:       configuredFieldConfig.FrontmatterKey,
					HeadingPath:          append([]string(nil), configuredFieldConfig.HeadingPath...),
					HTMLTitle:            configuredFieldConfig.HTMLTitle,
					MetaName:             configuredFieldConfig.MetaName,
					MetaProperty:         configuredFieldConfig.MetaProperty,
					Sensitivity:          configuredFieldConfig.Sensitivity,
					MaxInlineBytes:       configuredFieldConfig.MaxInlineBytes,
					AllowBlobRefFallback: configuredFieldConfig.AllowBlobRefFallback,
					AllowSuspiciousName:  configuredFieldConfig.AllowSuspiciousName,
				})
			}
			caps[configuredCapabilityDef.Name] = configuredCapability{
				Name:           configuredCapabilityDef.Name,
				Description:    configuredCapabilityDef.Description,
				Method:         configuredCapabilityDef.Method,
				Path:           configuredCapabilityDef.Path,
				ContentClass:   configuredCapabilityDef.ContentClass,
				Extractor:      configuredCapabilityDef.Extractor,
				ResponseFields: configuredCapabilityFields,
				ConnectionKey:  connectionKey,
			}
		}

		allowedHosts := make(map[string]struct{}, len(normalizedConfig.AllowedHosts))
		for _, allowedHost := range normalizedConfig.AllowedHosts {
			allowedHosts[allowedHost] = struct{}{}
		}

		var authorizationURL *url.URL
		if normalizedConfig.GrantType == controlapipkg.GrantTypePKCE {
			authorizationURL, err = parseAndValidateConfiguredURL("authorization_url", normalizedConfig.AuthorizationURL, allowedHosts)
			if err != nil {
				return nil, nil, err
			}
		}
		var tokenURL *url.URL
		if normalizedConfig.GrantType != controlapipkg.GrantTypePublicRead {
			tokenURL, err = parseAndValidateConfiguredURL("token_url", normalizedConfig.TokenURL, allowedHosts)
			if err != nil {
				return nil, nil, err
			}
		}
		apiBaseURL, err := parseAndValidateConfiguredURL("api_base_url", normalizedConfig.APIBaseURL, allowedHosts)
		if err != nil {
			return nil, nil, err
		}

		connectionDefinitions[connectionKey] = configuredConnection{
			Registration: connectionRegistration{
				Provider:   normalizedConfig.Provider,
				GrantType:  normalizedConfig.GrantType,
				Subject:    normalizedConfig.Subject,
				Scopes:     append([]string(nil), normalizedConfig.Scopes...),
				Credential: normalizedConfig.Credential,
			},
			ClientID:         normalizedConfig.ClientID,
			AuthorizationURL: authorizationURL,
			TokenURL:         tokenURL,
			RedirectURL:      normalizedConfig.RedirectURL,
			APIBaseURL:       apiBaseURL,
			AllowedHosts:     allowedHosts,
		}
		for capName, capDef := range caps {
			if _, exists := capabilityDefinitions[capName]; exists {
				return nil, nil, fmt.Errorf("duplicate configured capability %q", capName)
			}
			capabilityDefinitions[capName] = capDef
		}
	}

	return connectionDefinitions, capabilityDefinitions, nil
}

func normalizeConnectionConfig(rawConfig connectionConfigFile) (connectionConfigFile, error) {
	rawConfig.Provider = strings.TrimSpace(rawConfig.Provider)
	rawConfig.GrantType = strings.TrimSpace(rawConfig.GrantType)
	rawConfig.Subject = strings.TrimSpace(rawConfig.Subject)
	rawConfig.ClientID = strings.TrimSpace(rawConfig.ClientID)
	rawConfig.AuthorizationURL = strings.TrimSpace(rawConfig.AuthorizationURL)
	rawConfig.TokenURL = strings.TrimSpace(rawConfig.TokenURL)
	rawConfig.RedirectURL = strings.TrimSpace(rawConfig.RedirectURL)
	rawConfig.APIBaseURL = strings.TrimSpace(rawConfig.APIBaseURL)
	normalizedHosts, err := normalizeConfiguredHosts(rawConfig.AllowedHosts)
	if err != nil {
		return connectionConfigFile{}, err
	}
	rawConfig.AllowedHosts = normalizedHosts
	rawConfig.Scopes = normalizedConnectionScopes(rawConfig.Scopes)

	if err := (connectionRegistration{
		Provider:   rawConfig.Provider,
		GrantType:  rawConfig.GrantType,
		Subject:    rawConfig.Subject,
		Scopes:     rawConfig.Scopes,
		Credential: rawConfig.Credential,
	}).Validate(); err != nil {
		return connectionConfigFile{}, err
	}
	switch rawConfig.GrantType {
	case controlapipkg.GrantTypePublicRead:
		if rawConfig.ClientID != "" || rawConfig.AuthorizationURL != "" || rawConfig.TokenURL != "" || rawConfig.RedirectURL != "" {
			return connectionConfigFile{}, fmt.Errorf("public_read connections must not set client_id, authorization_url, token_url, or redirect_url")
		}
		if !secretRefIsEmpty(rawConfig.Credential) {
			return connectionConfigFile{}, fmt.Errorf("public_read connections must not define a credential ref")
		}
	case controlapipkg.GrantTypeClientCredentials:
	case controlapipkg.GrantTypePKCE:
		if _, err := parseAndValidateRedirectURL(rawConfig.RedirectURL); err != nil {
			return connectionConfigFile{}, err
		}
	default:
		return connectionConfigFile{}, fmt.Errorf("unsupported configured grant_type %q", rawConfig.GrantType)
	}
	if rawConfig.ClientID != "" {
		if err := identifiers.ValidateSafeIdentifier("connection client_id", rawConfig.ClientID); err != nil {
			return connectionConfigFile{}, err
		}
	}
	if len(rawConfig.AllowedHosts) == 0 {
		return connectionConfigFile{}, fmt.Errorf("allowed_hosts is required")
	}
	if len(rawConfig.Capabilities) == 0 {
		return connectionConfigFile{}, fmt.Errorf("at least one capability is required")
	}
	for capabilityIndex := range rawConfig.Capabilities {
		normalizedCapability, err := normalizeConnectionCapability(rawConfig.Capabilities[capabilityIndex])
		if err != nil {
			return connectionConfigFile{}, err
		}
		rawConfig.Capabilities[capabilityIndex] = normalizedCapability
	}
	return rawConfig, nil
}

func normalizeConnectionCapability(rawCapability connectionCapabilityConfig) (connectionCapabilityConfig, error) {
	rawCapability.Name = strings.TrimSpace(rawCapability.Name)
	rawCapability.Description = strings.TrimSpace(rawCapability.Description)
	rawCapability.Method = strings.ToUpper(strings.TrimSpace(rawCapability.Method))
	rawCapability.Path = strings.TrimSpace(rawCapability.Path)
	rawCapability.ContentClass = strings.TrimSpace(rawCapability.ContentClass)
	rawCapability.Extractor = strings.TrimSpace(rawCapability.Extractor)
	if err := identifiers.ValidateSafeIdentifier("configured capability name", rawCapability.Name); err != nil {
		return connectionCapabilityConfig{}, err
	}
	if rawCapability.Description == "" {
		return connectionCapabilityConfig{}, fmt.Errorf("configured capability description is required")
	}
	if rawCapability.Method != httpMethodGet {
		return connectionCapabilityConfig{}, fmt.Errorf("configured capability method must be GET")
	}
	if !strings.HasPrefix(rawCapability.Path, "/") || strings.Contains(rawCapability.Path, "..") {
		return connectionCapabilityConfig{}, fmt.Errorf("configured capability path must be absolute and traversal-free")
	}
	if strings.ContainsAny(rawCapability.Path, "?#") {
		return connectionCapabilityConfig{}, fmt.Errorf("configured capability path must not contain query or fragment components")
	}
	if len(rawCapability.ResponseFields) == 0 {
		return connectionCapabilityConfig{}, fmt.Errorf("configured capability response_fields is required")
	}
	switch rawCapability.ContentClass {
	case contentClassStructuredJSONConfig:
		if rawCapability.Extractor != extractorJSONFieldAllowlistConfig && rawCapability.Extractor != extractorJSONNestedSelectorConfig && rawCapability.Extractor != extractorJSONObjectListSelector {
			return connectionCapabilityConfig{}, fmt.Errorf("configured capability extractor must be %q, %q, or %q for content_class %q", extractorJSONFieldAllowlistConfig, extractorJSONNestedSelectorConfig, extractorJSONObjectListSelector, contentClassStructuredJSONConfig)
		}
	case contentClassMarkdownConfig:
		if rawCapability.Extractor != extractorMarkdownFrontmatterKeys && rawCapability.Extractor != extractorMarkdownSectionSelector {
			return connectionCapabilityConfig{}, fmt.Errorf("configured capability extractor must be %q or %q for content_class %q", extractorMarkdownFrontmatterKeys, extractorMarkdownSectionSelector, contentClassMarkdownConfig)
		}
	case contentClassHTMLConfig:
		if rawCapability.Extractor != extractorHTMLMetaAllowlistConfig {
			return connectionCapabilityConfig{}, fmt.Errorf("configured capability extractor must be %q for content_class %q", extractorHTMLMetaAllowlistConfig, contentClassHTMLConfig)
		}
	default:
		return connectionCapabilityConfig{}, fmt.Errorf("configured capability content_class must be %q, %q, or %q", contentClassStructuredJSONConfig, contentClassMarkdownConfig, contentClassHTMLConfig)
	}
	seenFieldNames := make(map[string]struct{}, len(rawCapability.ResponseFields))
	for fieldIndex := range rawCapability.ResponseFields {
		normalizedFieldConfig, err := normalizeConnectionCapabilityField(rawCapability.ContentClass, rawCapability.Extractor, rawCapability.ResponseFields[fieldIndex])
		if err != nil {
			return connectionCapabilityConfig{}, err
		}
		if _, exists := seenFieldNames[normalizedFieldConfig.Name]; exists {
			return connectionCapabilityConfig{}, fmt.Errorf("duplicate configured capability response field %q", normalizedFieldConfig.Name)
		}
		seenFieldNames[normalizedFieldConfig.Name] = struct{}{}
		rawCapability.ResponseFields[fieldIndex] = normalizedFieldConfig
	}
	return rawCapability, nil
}

func normalizeConnectionCapabilityField(contentClass string, extractorType string, rawFieldConfig connectionCapabilityFieldConfig) (connectionCapabilityFieldConfig, error) {
	rawFieldConfig.Name = strings.TrimSpace(rawFieldConfig.Name)
	rawFieldConfig.JSONField = strings.TrimSpace(rawFieldConfig.JSONField)
	rawFieldConfig.JSONPath = strings.TrimSpace(rawFieldConfig.JSONPath)
	for itemFieldIndex := range rawFieldConfig.JSONListItemFields {
		rawFieldConfig.JSONListItemFields[itemFieldIndex] = strings.TrimSpace(rawFieldConfig.JSONListItemFields[itemFieldIndex])
	}
	rawFieldConfig.FrontmatterKey = strings.TrimSpace(rawFieldConfig.FrontmatterKey)
	rawFieldConfig.MetaName = strings.TrimSpace(rawFieldConfig.MetaName)
	rawFieldConfig.MetaProperty = strings.TrimSpace(rawFieldConfig.MetaProperty)
	for headingIndex := range rawFieldConfig.HeadingPath {
		rawFieldConfig.HeadingPath[headingIndex] = strings.TrimSpace(rawFieldConfig.HeadingPath[headingIndex])
	}
	rawFieldConfig.Sensitivity = strings.TrimSpace(rawFieldConfig.Sensitivity)
	if err := identifiers.ValidateSafeIdentifier("configured capability response field", rawFieldConfig.Name); err != nil {
		return connectionCapabilityFieldConfig{}, err
	}
	if isSuspiciousRemoteFieldName(rawFieldConfig.Name) && !rawFieldConfig.AllowSuspiciousName {
		return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q uses a suspicious remote field name; set allow_suspicious_name explicitly to permit it", rawFieldConfig.Name)
	}
	switch rawFieldConfig.Sensitivity {
	case controlapipkg.ResultFieldSensitivityBenign, controlapipkg.ResultFieldSensitivityTaintedText:
	default:
		return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field sensitivity must be %q or %q", controlapipkg.ResultFieldSensitivityBenign, controlapipkg.ResultFieldSensitivityTaintedText)
	}
	if rawFieldConfig.MaxInlineBytes <= 0 || rawFieldConfig.MaxInlineBytes > maxResponseBodyBytes {
		return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field max_inline_bytes must be between 1 and %d", maxResponseBodyBytes)
	}
	switch {
	case contentClass == contentClassStructuredJSONConfig && extractorType == extractorJSONFieldAllowlistConfig:
		if strings.TrimSpace(rawFieldConfig.JSONField) == "" {
			rawFieldConfig.JSONField = rawFieldConfig.Name
		}
		if err := identifiers.ValidateSafeIdentifier("configured capability json_field", rawFieldConfig.JSONField); err != nil {
			return connectionCapabilityFieldConfig{}, err
		}
		if rawFieldConfig.JSONPath != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json_path for json allowlist extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.FrontmatterKey != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set frontmatter_key for json extraction", rawFieldConfig.Name)
		}
		if len(rawFieldConfig.HeadingPath) != 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set heading_path for json extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.HTMLTitle || rawFieldConfig.MetaName != "" || rawFieldConfig.MetaProperty != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set html selectors for json extraction", rawFieldConfig.Name)
		}
	case contentClass == contentClassStructuredJSONConfig && extractorType == extractorJSONNestedSelectorConfig:
		if strings.TrimSpace(rawFieldConfig.JSONPath) == "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q requires json_path", rawFieldConfig.Name)
		}
		if err := validateJSONSelectorPath(rawFieldConfig.JSONPath); err != nil {
			return connectionCapabilityFieldConfig{}, err
		}
		if rawFieldConfig.JSONField != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json_field for json nested extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.FrontmatterKey != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set frontmatter_key for json nested extraction", rawFieldConfig.Name)
		}
		if len(rawFieldConfig.HeadingPath) != 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set heading_path for json nested extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.HTMLTitle || rawFieldConfig.MetaName != "" || rawFieldConfig.MetaProperty != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set html selectors for json nested extraction", rawFieldConfig.Name)
		}
		if len(rawFieldConfig.JSONListItemFields) != 0 || rawFieldConfig.MaxItems != 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json list fields for json nested extraction", rawFieldConfig.Name)
		}
	case contentClass == contentClassStructuredJSONConfig && extractorType == extractorJSONObjectListSelector:
		if strings.TrimSpace(rawFieldConfig.JSONPath) == "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q requires json_path", rawFieldConfig.Name)
		}
		if err := validateJSONSelectorPath(rawFieldConfig.JSONPath); err != nil {
			return connectionCapabilityFieldConfig{}, err
		}
		if len(rawFieldConfig.JSONListItemFields) == 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q requires json_list_item_fields", rawFieldConfig.Name)
		}
		seenItemFields := make(map[string]struct{}, len(rawFieldConfig.JSONListItemFields))
		for _, rawItemField := range rawFieldConfig.JSONListItemFields {
			if rawItemField == "" {
				return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q json_list_item_fields must not contain empty values", rawFieldConfig.Name)
			}
			if err := identifiers.ValidateSafeIdentifier("configured capability json_list_item_field", rawItemField); err != nil {
				return connectionCapabilityFieldConfig{}, err
			}
			if _, exists := seenItemFields[rawItemField]; exists {
				return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q has duplicate json_list_item_field %q", rawFieldConfig.Name, rawItemField)
			}
			seenItemFields[rawItemField] = struct{}{}
		}
		if rawFieldConfig.MaxItems <= 0 || rawFieldConfig.MaxItems > maxConfiguredListItems {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q max_items must be between 1 and %d", rawFieldConfig.Name, maxConfiguredListItems)
		}
		if rawFieldConfig.JSONField != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json_field for json object list extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.FrontmatterKey != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set frontmatter_key for json object list extraction", rawFieldConfig.Name)
		}
		if len(rawFieldConfig.HeadingPath) != 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set heading_path for json object list extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.HTMLTitle || rawFieldConfig.MetaName != "" || rawFieldConfig.MetaProperty != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set html selectors for json object list extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.Sensitivity != controlapipkg.ResultFieldSensitivityTaintedText {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q json object list extraction must use sensitivity %q", rawFieldConfig.Name, controlapipkg.ResultFieldSensitivityTaintedText)
		}
	case contentClass == contentClassMarkdownConfig && extractorType == extractorMarkdownFrontmatterKeys:
		if strings.TrimSpace(rawFieldConfig.FrontmatterKey) == "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q requires frontmatter_key", rawFieldConfig.Name)
		}
		if err := identifiers.ValidateSafeIdentifier("configured capability frontmatter_key", rawFieldConfig.FrontmatterKey); err != nil {
			return connectionCapabilityFieldConfig{}, err
		}
		if rawFieldConfig.JSONField != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json_field for markdown frontmatter extraction", rawFieldConfig.Name)
		}
		if len(rawFieldConfig.HeadingPath) != 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set heading_path for markdown frontmatter extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.HTMLTitle || rawFieldConfig.MetaName != "" || rawFieldConfig.MetaProperty != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set html selectors for markdown frontmatter extraction", rawFieldConfig.Name)
		}
		if len(rawFieldConfig.JSONListItemFields) != 0 || rawFieldConfig.MaxItems != 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json list fields for markdown frontmatter extraction", rawFieldConfig.Name)
		}
	case contentClass == contentClassMarkdownConfig && extractorType == extractorMarkdownSectionSelector:
		if len(rawFieldConfig.HeadingPath) == 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q requires heading_path", rawFieldConfig.Name)
		}
		for _, rawHeadingPart := range rawFieldConfig.HeadingPath {
			if strings.TrimSpace(rawHeadingPart) == "" {
				return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q heading_path must not contain empty elements", rawFieldConfig.Name)
			}
			if strings.ContainsAny(rawHeadingPart, "\r\n") {
				return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q heading_path must be single-line", rawFieldConfig.Name)
			}
		}
		if rawFieldConfig.JSONField != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json_field for markdown section extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.FrontmatterKey != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set frontmatter_key for markdown section extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.HTMLTitle || rawFieldConfig.MetaName != "" || rawFieldConfig.MetaProperty != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set html selectors for markdown section extraction", rawFieldConfig.Name)
		}
		if len(rawFieldConfig.JSONListItemFields) != 0 || rawFieldConfig.MaxItems != 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json list fields for markdown section extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.Sensitivity != controlapipkg.ResultFieldSensitivityTaintedText {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q markdown section extraction must use sensitivity %q", rawFieldConfig.Name, controlapipkg.ResultFieldSensitivityTaintedText)
		}
	case contentClass == contentClassHTMLConfig && extractorType == extractorHTMLMetaAllowlistConfig:
		selectorCount := 0
		if rawFieldConfig.HTMLTitle {
			selectorCount++
		}
		if rawFieldConfig.MetaName != "" {
			selectorCount++
			if err := validateHTMLMetadataSelector("meta_name", rawFieldConfig.MetaName); err != nil {
				return connectionCapabilityFieldConfig{}, err
			}
		}
		if rawFieldConfig.MetaProperty != "" {
			selectorCount++
			if err := validateHTMLMetadataSelector("meta_property", rawFieldConfig.MetaProperty); err != nil {
				return connectionCapabilityFieldConfig{}, err
			}
		}
		if selectorCount != 1 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must set exactly one of html_title, meta_name, or meta_property", rawFieldConfig.Name)
		}
		if rawFieldConfig.JSONField != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json_field for html metadata extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.FrontmatterKey != "" {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set frontmatter_key for html metadata extraction", rawFieldConfig.Name)
		}
		if len(rawFieldConfig.HeadingPath) != 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set heading_path for html metadata extraction", rawFieldConfig.Name)
		}
		if len(rawFieldConfig.JSONListItemFields) != 0 || rawFieldConfig.MaxItems != 0 {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q must not set json list fields for html metadata extraction", rawFieldConfig.Name)
		}
		if rawFieldConfig.Sensitivity != controlapipkg.ResultFieldSensitivityTaintedText {
			return connectionCapabilityFieldConfig{}, fmt.Errorf("configured capability response field %q html metadata extraction must use sensitivity %q", rawFieldConfig.Name, controlapipkg.ResultFieldSensitivityTaintedText)
		}
	default:
		return connectionCapabilityFieldConfig{}, fmt.Errorf("unsupported configured capability extractor combination")
	}
	return rawFieldConfig, nil
}

func validateJSONSelectorPath(rawPath string) error {
	pathParts := strings.Split(rawPath, ".")
	if len(pathParts) < 2 {
		return fmt.Errorf("json_path must contain at least one dot-separated nested field")
	}
	for _, pathPart := range pathParts {
		if strings.TrimSpace(pathPart) == "" {
			return fmt.Errorf("json_path must not contain empty path elements")
		}
		if err := identifiers.ValidateSafeIdentifier("configured capability json_path element", pathPart); err != nil {
			return err
		}
	}
	return nil
}

func validateHTMLMetadataSelector(fieldLabel string, rawValue string) error {
	if rawValue == "" {
		return fmt.Errorf("%s is required", fieldLabel)
	}
	if strings.ContainsAny(rawValue, "\r\n\t ") {
		return fmt.Errorf("%s must be single-token and whitespace-free", fieldLabel)
	}
	if len(rawValue) > 128 {
		return fmt.Errorf("%s must be <= 128 bytes", fieldLabel)
	}
	return nil
}

func isSuspiciousRemoteFieldName(fieldName string) bool {
	switch fieldName {
	case "approved", "policy", "tool_call", "instructions", "memory_candidate":
		return true
	default:
		return false
	}
}

func contentTypeMatchesCapability(contentTypeHeader string, configuredCapabilityDefinition configuredCapability) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentTypeHeader))
	if err != nil {
		return false
	}
	switch configuredCapabilityDefinition.ContentClass {
	case contentClassStructuredJSONConfig:
		return mediaType == contentTypeApplicationJSON
	case contentClassMarkdownConfig:
		return mediaType == "text/markdown" || mediaType == "text/x-markdown"
	case contentClassHTMLConfig:
		return mediaType == "text/html"
	default:
		return false
	}
}

func normalizeConfiguredHosts(rawHosts []string) ([]string, error) {
	seenHosts := make(map[string]struct{}, len(rawHosts))
	normalizedHosts := make([]string, 0, len(rawHosts))
	for _, rawHost := range rawHosts {
		trimmedHost := strings.TrimSpace(rawHost)
		if trimmedHost == "" {
			continue
		}
		if err := identifiers.ValidateSafeIdentifier("allowed host", trimmedHost); err != nil {
			return nil, err
		}
		if _, found := seenHosts[trimmedHost]; found {
			continue
		}
		seenHosts[trimmedHost] = struct{}{}
		normalizedHosts = append(normalizedHosts, trimmedHost)
	}
	sort.Strings(normalizedHosts)
	return normalizedHosts, nil
}

func parseAndValidateConfiguredURL(fieldName string, rawValue string, allowedHosts map[string]struct{}) (*url.URL, error) {
	parsedURL, err := url.Parse(rawValue)
	if err != nil {
		return nil, fmt.Errorf("%s is invalid: %w", fieldName, err)
	}
	if !parsedURL.IsAbs() {
		return nil, fmt.Errorf("%s must be absolute", fieldName)
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, fmt.Errorf("%s must not contain query or fragment data", fieldName)
	}
	if parsedURL.Path == "" {
		parsedURL.Path = "/"
	}
	if strings.Contains(parsedURL.Path, "..") {
		return nil, fmt.Errorf("%s contains traversal patterns", fieldName)
	}
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return nil, fmt.Errorf("%s must include a host", fieldName)
	}
	if _, allowed := allowedHosts[hostname]; !allowed {
		return nil, fmt.Errorf("%s host %q is not in allowed_hosts", fieldName, hostname)
	}
	if parsedURL.Scheme != "https" && !isLocalhostHost(hostname) {
		return nil, fmt.Errorf("%s must use https for non-local hosts", fieldName)
	}
	return parsedURL, nil
}

func parseAndValidateRedirectURL(rawValue string) (*url.URL, error) {
	parsedURL, err := url.Parse(rawValue)
	if err != nil {
		return nil, fmt.Errorf("redirect_url is invalid: %w", err)
	}
	if !parsedURL.IsAbs() {
		return nil, fmt.Errorf("redirect_url must be absolute")
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, fmt.Errorf("redirect_url must not contain query or fragment data")
	}
	if parsedURL.Scheme == "https" {
		return parsedURL, nil
	}
	if parsedURL.Scheme == "http" && isLocalhostHost(parsedURL.Hostname()) {
		return parsedURL, nil
	}
	return nil, fmt.Errorf("redirect_url must use https or localhost http")
}

func isLocalhostHost(hostname string) bool {
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return true
	}
	return net.ParseIP(hostname) != nil && net.ParseIP(hostname).IsLoopback()
}

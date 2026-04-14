package loopgate

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	errSiteURLInvalid                   = fmt.Errorf("site_url_invalid")
	errSiteHTTPSRequired                = fmt.Errorf("https_required")
	errSiteTrustDraftUnavailable        = fmt.Errorf("site_trust_draft_not_available")
	errSiteTrustDraftAlreadyExists      = fmt.Errorf("site_trust_draft_exists")
	errSiteInspectionContentUnsupported = fmt.Errorf("site_inspection_unsupported_content_type")
)

type validatedSiteTarget struct {
	NormalizedURL string
	Scheme        string
	Hostname      string
	Authority     string
	Path          string
}

type inspectedSite struct {
	Target         validatedSiteTarget
	HTTPStatusCode int
	ContentType    string
	HTTPS          bool
	TLSValid       bool
	Certificate    *SiteCertificateInfo
	RawBody        string
}

func validateSiteTarget(rawURL string) (validatedSiteTarget, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return validatedSiteTarget{}, fmt.Errorf("%w: parse url: %v", errSiteURLInvalid, err)
	}
	if parsedURL.User != nil {
		return validatedSiteTarget{}, fmt.Errorf("%w: userinfo is not allowed", errSiteURLInvalid)
	}
	if parsedURL.RawQuery != "" {
		return validatedSiteTarget{}, fmt.Errorf("%w: query is not allowed", errSiteURLInvalid)
	}
	if parsedURL.Fragment != "" {
		return validatedSiteTarget{}, fmt.Errorf("%w: fragment is not allowed", errSiteURLInvalid)
	}
	scheme := strings.ToLower(strings.TrimSpace(parsedURL.Scheme))
	if scheme != "https" && scheme != "http" {
		return validatedSiteTarget{}, fmt.Errorf("%w: unsupported scheme %q", errSiteURLInvalid, parsedURL.Scheme)
	}
	hostname := strings.TrimSpace(parsedURL.Hostname())
	if hostname == "" {
		return validatedSiteTarget{}, fmt.Errorf("%w: host is required", errSiteURLInvalid)
	}
	path := parsedURL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.Contains(path, "..") {
		return validatedSiteTarget{}, fmt.Errorf("%w: path traversal is not allowed", errSiteURLInvalid)
	}
	if scheme == "http" && !isLocalhostHost(hostname) {
		return validatedSiteTarget{}, fmt.Errorf("%w: only localhost http is allowed", errSiteHTTPSRequired)
	}
	normalizedURL := (&url.URL{
		Scheme: scheme,
		Host:   parsedURL.Host,
		Path:   path,
	}).String()
	return validatedSiteTarget{
		NormalizedURL: normalizedURL,
		Scheme:        scheme,
		Hostname:      hostname,
		Authority:     parsedURL.Host,
		Path:          path,
	}, nil
}

func (server *Server) inspectSite(ctx context.Context, rawURL string) (SiteInspectionResponse, error) {
	validatedSite, err := validateSiteTarget(rawURL)
	if err != nil {
		return SiteInspectionResponse{}, err
	}

	inspectedSiteResponse, err := server.fetchSiteInspection(ctx, validatedSite)
	if err != nil {
		return SiteInspectionResponse{}, err
	}

	siteInspectionResponse := SiteInspectionResponse{
		NormalizedURL:  inspectedSiteResponse.Target.NormalizedURL,
		Scheme:         inspectedSiteResponse.Target.Scheme,
		Host:           inspectedSiteResponse.Target.Authority,
		Path:           inspectedSiteResponse.Target.Path,
		HTTPStatusCode: inspectedSiteResponse.HTTPStatusCode,
		ContentType:    inspectedSiteResponse.ContentType,
		HTTPS:          inspectedSiteResponse.HTTPS,
		TLSValid:       inspectedSiteResponse.TLSValid,
		Certificate:    inspectedSiteResponse.Certificate,
	}
	if siteInspectionResponse.HTTPS && !siteInspectionResponse.TLSValid {
		return siteInspectionResponse, nil
	}

	draftSuggestion, err := buildSiteTrustDraftSuggestion(inspectedSiteResponse)
	if err != nil {
		if err == errSiteTrustDraftUnavailable || err == errSiteInspectionContentUnsupported {
			return siteInspectionResponse, nil
		}
		return SiteInspectionResponse{}, err
	}
	siteInspectionResponse.DraftSuggestion = &draftSuggestion
	siteInspectionResponse.TrustDraftAllowed = siteTrustDraftTransportAllowed(siteInspectionResponse)
	return siteInspectionResponse, nil
}

func (server *Server) createSiteTrustDraft(ctx context.Context, tokenClaims capabilityToken, rawURL string) (SiteTrustDraftResponse, error) {
	siteInspectionResponse, err := server.inspectSite(ctx, rawURL)
	if err != nil {
		return SiteTrustDraftResponse{}, err
	}
	if !siteInspectionResponse.TrustDraftAllowed || siteInspectionResponse.DraftSuggestion == nil {
		if !siteTrustDraftTransportAllowed(siteInspectionResponse) {
			return SiteTrustDraftResponse{}, errSiteHTTPSRequired
		}
		return SiteTrustDraftResponse{}, errSiteTrustDraftUnavailable
	}

	draftSuggestion := *siteInspectionResponse.DraftSuggestion
	draftPath := filepath.Join(server.repoRoot, connectionConfigDir, "drafts", draftSuggestion.Provider+"-"+draftSuggestion.Subject+".yaml")
	if _, err := os.Stat(draftPath); err == nil {
		return SiteTrustDraftResponse{}, errSiteTrustDraftAlreadyExists
	} else if err != nil && !os.IsNotExist(err) {
		return SiteTrustDraftResponse{}, fmt.Errorf("stat trust draft: %w", err)
	}

	draftConfig := buildSiteTrustDraftConfig(siteInspectionResponse, draftSuggestion)
	if err := writeSiteTrustDraftFile(draftPath, draftConfig); err != nil {
		return SiteTrustDraftResponse{}, err
	}
	if err := server.logEvent("site.trust_draft_created", tokenClaims.ControlSessionID, map[string]interface{}{
		"normalized_url":       siteInspectionResponse.NormalizedURL,
		"provider":             draftSuggestion.Provider,
		"subject":              draftSuggestion.Subject,
		"draft_path":           strings.TrimPrefix(draftPath, server.repoRoot+string(filepath.Separator)),
		"content_class":        draftSuggestion.ContentClass,
		"extractor":            draftSuggestion.Extractor,
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
	}); err != nil {
		_ = os.Remove(draftPath)
		return SiteTrustDraftResponse{}, err
	}

	return SiteTrustDraftResponse{
		NormalizedURL: siteInspectionResponse.NormalizedURL,
		DraftPath:     draftPath,
		Suggestion:    draftSuggestion,
	}, nil
}

func (server *Server) fetchSiteInspection(ctx context.Context, validatedSite validatedSiteTarget) (inspectedSite, error) {
	inspected := inspectedSite{
		Target: validatedSite,
		HTTPS:  validatedSite.Scheme == "https",
	}
	inspectionClient := server.siteInspectionHTTPClient(validatedSite, &inspected)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, validatedSite.NormalizedURL, nil)
	if err != nil {
		return inspectedSite{}, fmt.Errorf("build inspection request: %w", err)
	}

	httpResponse, err := inspectionClient.Do(httpRequest)
	if err != nil {
		if inspected.HTTPS && inspected.Certificate != nil && !inspected.TLSValid {
			return inspected, nil
		}
		return inspectedSite{}, fmt.Errorf("inspect site request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	rawBodyBytes, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBodyBytes))
	if err != nil {
		return inspectedSite{}, fmt.Errorf("read inspection response body: %w", err)
	}

	normalizedContentType := strings.TrimSpace(httpResponse.Header.Get("Content-Type"))
	if mediaType, _, err := mime.ParseMediaType(normalizedContentType); err == nil {
		normalizedContentType = mediaType
	}

	inspected.HTTPStatusCode = httpResponse.StatusCode
	inspected.ContentType = normalizedContentType
	inspected.RawBody = string(rawBodyBytes)

	if httpResponse.TLS != nil && len(httpResponse.TLS.PeerCertificates) > 0 {
		if inspected.Certificate == nil {
			inspected.Certificate = certificateInfoForLeaf(httpResponse.TLS.PeerCertificates[0])
		}
		if !inspected.TLSValid {
			inspected.TLSValid = verifySiteCertificate(validatedSite.Hostname, httpResponse.TLS.PeerCertificates, nil) == nil
		}
	}
	if validatedSite.Scheme == "http" && isLocalhostHost(validatedSite.Hostname) {
		inspected.TLSValid = false
	}
	return inspected, nil
}

func (server *Server) siteInspectionHTTPClient(validatedSite validatedSiteTarget, inspected *inspectedSite) *http.Client {
	timeout := 10 * time.Second
	policyRuntime := server.currentPolicyRuntime()
	if policyRuntime.httpClient != nil && policyRuntime.httpClient.Timeout > 0 {
		timeout = policyRuntime.httpClient.Timeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if policyRuntime.httpClient != nil {
		if configuredTransport, ok := policyRuntime.httpClient.Transport.(*http.Transport); ok && configuredTransport != nil {
			transport = configuredTransport.Clone()
		}
	}
	if validatedSite.Scheme == "https" {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		}
		rootPool := transport.TLSClientConfig.RootCAs
		transport.TLSClientConfig.InsecureSkipVerify = true
		transport.TLSClientConfig.VerifyConnection = func(connectionState tls.ConnectionState) error {
			if len(connectionState.PeerCertificates) > 0 {
				inspected.Certificate = certificateInfoForLeaf(connectionState.PeerCertificates[0])
			}
			verifyErr := verifySiteCertificate(validatedSite.Hostname, connectionState.PeerCertificates, rootPool)
			inspected.TLSValid = verifyErr == nil
			return verifyErr
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if len(via) == 0 {
				return nil
			}
			origin := via[0].URL
			if !strings.EqualFold(request.URL.Host, origin.Host) {
				return fmt.Errorf("redirect changed host")
			}
			if !strings.EqualFold(request.URL.Scheme, origin.Scheme) {
				return fmt.Errorf("redirect changed scheme")
			}
			return nil
		},
	}
}

func certificateInfoForLeaf(leafCertificate *x509.Certificate) *SiteCertificateInfo {
	if leafCertificate == nil {
		return nil
	}
	fingerprint := sha256.Sum256(leafCertificate.Raw)
	return &SiteCertificateInfo{
		Subject:           leafCertificate.Subject.String(),
		Issuer:            leafCertificate.Issuer.String(),
		DNSNames:          append([]string(nil), leafCertificate.DNSNames...),
		NotBeforeUTC:      leafCertificate.NotBefore.UTC().Format(time.RFC3339Nano),
		NotAfterUTC:       leafCertificate.NotAfter.UTC().Format(time.RFC3339Nano),
		FingerprintSHA256: hex.EncodeToString(fingerprint[:]),
	}
}

func verifySiteCertificate(hostname string, peerCertificates []*x509.Certificate, rootPool *x509.CertPool) error {
	if len(peerCertificates) == 0 {
		return fmt.Errorf("missing peer certificate")
	}
	if rootPool == nil {
		systemRootPool, err := x509.SystemCertPool()
		if err != nil || systemRootPool == nil {
			systemRootPool = x509.NewCertPool()
		}
		rootPool = systemRootPool
	}
	intermediatePool := x509.NewCertPool()
	for _, intermediateCertificate := range peerCertificates[1:] {
		intermediatePool.AddCert(intermediateCertificate)
	}
	leafCertificate := peerCertificates[0]
	if _, err := leafCertificate.Verify(x509.VerifyOptions{
		Roots:         rootPool,
		Intermediates: intermediatePool,
		CurrentTime:   time.Now(),
	}); err != nil {
		return err
	}
	return leafCertificate.VerifyHostname(hostname)
}

func buildSiteTrustDraftSuggestion(inspected inspectedSite) (SiteTrustDraftSuggestion, error) {
	if inspected.HTTPStatusCode < 200 || inspected.HTTPStatusCode >= 300 {
		return SiteTrustDraftSuggestion{}, errSiteTrustDraftUnavailable
	}

	switch {
	case inspected.ContentType == "application/json" || strings.HasSuffix(inspected.ContentType, "+json"):
		return buildJSONSiteTrustDraftSuggestion(inspected)
	case inspected.ContentType == "text/html":
		return buildHTMLSiteTrustDraftSuggestion(inspected)
	default:
		return SiteTrustDraftSuggestion{}, errSiteInspectionContentUnsupported
	}
}

func siteTrustDraftTransportAllowed(siteInspectionResponse SiteInspectionResponse) bool {
	if siteInspectionResponse.Scheme == "https" {
		return siteInspectionResponse.TLSValid
	}
	return siteInspectionResponse.Scheme == "http" && isLocalhostHost(validatedHostForDraft(siteInspectionResponse.Host))
}

func buildJSONSiteTrustDraftSuggestion(inspected inspectedSite) (SiteTrustDraftSuggestion, error) {
	var parsedBody map[string]interface{}
	if err := json.Unmarshal([]byte(inspected.RawBody), &parsedBody); err != nil {
		return SiteTrustDraftSuggestion{}, errSiteTrustDraftUnavailable
	}
	statusDescription, err := extractNestedJSONField(parsedBody, "status.description")
	if err != nil {
		return SiteTrustDraftSuggestion{}, errSiteTrustDraftUnavailable
	}
	statusIndicator, err := extractNestedJSONField(parsedBody, "status.indicator")
	if err != nil {
		return SiteTrustDraftSuggestion{}, errSiteTrustDraftUnavailable
	}
	descriptionValue, okDescription := statusDescription.(string)
	indicatorValue, okIndicator := statusIndicator.(string)
	if !okDescription || !okIndicator || strings.TrimSpace(descriptionValue) == "" || strings.TrimSpace(indicatorValue) == "" {
		return SiteTrustDraftSuggestion{}, errSiteTrustDraftUnavailable
	}

	providerID, subjectID := siteDraftIdentifiers(inspected.Target)
	return SiteTrustDraftSuggestion{
		Provider:       providerID,
		Subject:        subjectID,
		CapabilityName: providerID + ".status_get",
		ContentClass:   contentClassStructuredJSONConfig,
		Extractor:      extractorJSONNestedSelectorConfig,
		CapabilityPath: inspected.Target.Path,
		Fields: []SiteDraftField{
			{
				Name:           "status_description",
				Sensitivity:    ResultFieldSensitivityTaintedText,
				MaxInlineBytes: 256,
				JSONPath:       "status.description",
			},
			{
				Name:           "status_indicator",
				Sensitivity:    ResultFieldSensitivityTaintedText,
				MaxInlineBytes: 64,
				JSONPath:       "status.indicator",
			},
		},
	}, nil
}

func buildHTMLSiteTrustDraftSuggestion(inspected inspectedSite) (SiteTrustDraftSuggestion, error) {
	parsedHTMLMetadata, err := parseHTMLMetadata(inspected.RawBody)
	if err != nil {
		return SiteTrustDraftSuggestion{}, errSiteTrustDraftUnavailable
	}

	draftFields := make([]SiteDraftField, 0, 3)
	if len(parsedHTMLMetadata.Titles) == 1 && strings.TrimSpace(parsedHTMLMetadata.Titles[0]) != "" {
		draftFields = append(draftFields, SiteDraftField{
			Name:           "page_title",
			Sensitivity:    ResultFieldSensitivityTaintedText,
			MaxInlineBytes: 256,
			HTMLTitle:      true,
		})
	}
	if descriptionValues, found := parsedHTMLMetadata.MetaNameValues["description"]; found && len(descriptionValues.Values) == 1 && strings.TrimSpace(descriptionValues.Values[0]) != "" {
		draftFields = append(draftFields, SiteDraftField{
			Name:           "description",
			Sensitivity:    ResultFieldSensitivityTaintedText,
			MaxInlineBytes: 256,
			MetaName:       "description",
		})
	}
	if siteNameValues, found := parsedHTMLMetadata.MetaPropertyValues["og:site_name"]; found && len(siteNameValues.Values) == 1 && strings.TrimSpace(siteNameValues.Values[0]) != "" {
		draftFields = append(draftFields, SiteDraftField{
			Name:           "site_name",
			Sensitivity:    ResultFieldSensitivityTaintedText,
			MaxInlineBytes: 128,
			MetaProperty:   "og:site_name",
		})
	}
	if len(draftFields) == 0 {
		return SiteTrustDraftSuggestion{}, errSiteTrustDraftUnavailable
	}

	providerID, subjectID := siteDraftIdentifiers(inspected.Target)
	return SiteTrustDraftSuggestion{
		Provider:       providerID,
		Subject:        subjectID,
		CapabilityName: providerID + ".page_get",
		ContentClass:   contentClassHTMLConfig,
		Extractor:      extractorHTMLMetaAllowlistConfig,
		CapabilityPath: inspected.Target.Path,
		Fields:         draftFields,
	}, nil
}

func siteDraftIdentifiers(validatedSite validatedSiteTarget) (string, string) {
	providerID := sanitizeSiteIdentifier(validatedSite.Hostname)
	pathLabel := "root"
	if trimmedPath := strings.Trim(strings.TrimSpace(validatedSite.Path), "/"); trimmedPath != "" {
		pathLabel = sanitizeSiteIdentifier(strings.ReplaceAll(trimmedPath, "/", "."))
	}
	return providerID, pathLabel
}

func sanitizeSiteIdentifier(rawValue string) string {
	trimmedValue := strings.TrimSpace(strings.ToLower(rawValue))
	if trimmedValue == "" {
		return "site"
	}
	replacer := strings.NewReplacer("/", ".", "\\", ".", " ", "-", "@", "-", "%", "-", "+", "-", "=", "-", ",", "-", ";", "-", ":", ".", "[", "-", "]", "-", "{", "-", "}", "-", "(", "-", ")", "-", "&", "-", "?", "-", "#", "-", "!", "-", "~", "-", "*", "-", "\"", "-", "'", "-", "|", "-", "<", "-", ">", "-", "^", "-", "`", "-")
	normalizedValue := replacer.Replace(trimmedValue)
	normalizedRunes := make([]rune, 0, len(normalizedValue))
	for _, rawRune := range normalizedValue {
		switch {
		case rawRune >= 'a' && rawRune <= 'z':
			normalizedRunes = append(normalizedRunes, rawRune)
		case rawRune >= '0' && rawRune <= '9':
			normalizedRunes = append(normalizedRunes, rawRune)
		case rawRune == '.' || rawRune == '-' || rawRune == '_' || rawRune == ':':
			normalizedRunes = append(normalizedRunes, rawRune)
		}
	}
	candidate := strings.Trim(strings.ReplaceAll(string(normalizedRunes), "..", "."), ".-_:")
	if candidate == "" {
		candidate = "site"
	}
	if candidate[0] < 'a' || candidate[0] > 'z' {
		candidate = "site-" + candidate
	}
	if len(candidate) > 64 {
		digest := sha256.Sum256([]byte(trimmedValue))
		candidate = candidate[:55] + "-" + hex.EncodeToString(digest[:])[:8]
	}
	return candidate
}

func buildSiteTrustDraftConfig(siteInspectionResponse SiteInspectionResponse, draftSuggestion SiteTrustDraftSuggestion) connectionConfigFile {
	draftFields := make([]connectionCapabilityFieldConfig, 0, len(draftSuggestion.Fields))
	for _, draftField := range draftSuggestion.Fields {
		draftFields = append(draftFields, connectionCapabilityFieldConfig{
			Name:                 draftField.Name,
			JSONPath:             draftField.JSONPath,
			HTMLTitle:            draftField.HTMLTitle,
			MetaName:             draftField.MetaName,
			MetaProperty:         draftField.MetaProperty,
			Sensitivity:          draftField.Sensitivity,
			MaxInlineBytes:       draftField.MaxInlineBytes,
			AllowBlobRefFallback: draftField.AllowBlobRefFallback,
		})
	}
	return connectionConfigFile{
		Provider:     draftSuggestion.Provider,
		GrantType:    GrantTypePublicRead,
		Subject:      draftSuggestion.Subject,
		APIBaseURL:   (&url.URL{Scheme: siteInspectionResponse.Scheme, Host: siteInspectionResponse.Host}).String(),
		AllowedHosts: []string{validatedHostForDraft(siteInspectionResponse.Host)},
		Capabilities: []connectionCapabilityConfig{{
			Name:           draftSuggestion.CapabilityName,
			Description:    fmt.Sprintf("Read trusted public site data from %s.", siteInspectionResponse.NormalizedURL),
			Method:         httpMethodGet,
			Path:           draftSuggestion.CapabilityPath,
			ContentClass:   draftSuggestion.ContentClass,
			Extractor:      draftSuggestion.Extractor,
			ResponseFields: draftFields,
		}},
	}
}

func validatedHostForDraft(authority string) string {
	parsedHost := strings.TrimSpace(authority)
	if hostName, _, err := net.SplitHostPort(parsedHost); err == nil && strings.TrimSpace(hostName) != "" {
		return strings.Trim(hostName, "[]")
	}
	if strings.HasPrefix(parsedHost, "[") && strings.HasSuffix(parsedHost, "]") {
		return strings.Trim(parsedHost, "[]")
	}
	if parsedHost != "" {
		return parsedHost
	}
	return ""
}

func writeSiteTrustDraftFile(path string, draftConfig connectionConfigFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create trust draft dir: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return errSiteTrustDraftAlreadyExists
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat trust draft: %w", err)
	}

	encodedBytes, err := yaml.Marshal(&draftConfig)
	if err != nil {
		return fmt.Errorf("marshal trust draft: %w", err)
	}
	tempPath := path + ".tmp"
	draftFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open trust draft temp file: %w", err)
	}
	defer func() { _ = draftFile.Close() }()
	if _, err := draftFile.Write(encodedBytes); err != nil {
		return fmt.Errorf("write trust draft temp file: %w", err)
	}
	if len(encodedBytes) == 0 || encodedBytes[len(encodedBytes)-1] != '\n' {
		if _, err := io.WriteString(draftFile, "\n"); err != nil {
			return fmt.Errorf("write trust draft newline: %w", err)
		}
	}
	if err := draftFile.Sync(); err != nil {
		return fmt.Errorf("sync trust draft temp file: %w", err)
	}
	if err := draftFile.Close(); err != nil {
		return fmt.Errorf("close trust draft temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename trust draft temp file: %w", err)
	}
	if draftDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = draftDir.Sync()
		_ = draftDir.Close()
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod trust draft file: %w", err)
	}
	return nil
}

func siteInspectionHTTPStatus(err error) int {
	switch {
	case errors.Is(err, errSiteURLInvalid), errors.Is(err, errSiteHTTPSRequired), errors.Is(err, errSiteInspectionContentUnsupported):
		return http.StatusBadRequest
	case errors.Is(err, errSiteTrustDraftUnavailable):
		return http.StatusConflict
	case errors.Is(err, errSiteTrustDraftAlreadyExists):
		return http.StatusConflict
	default:
		return http.StatusBadGateway
	}
}

func siteTrustDenialCode(err error) string {
	switch {
	case errors.Is(err, errSiteURLInvalid):
		return DenialCodeSiteURLInvalid
	case errors.Is(err, errSiteHTTPSRequired):
		return DenialCodeHTTPSRequired
	case errors.Is(err, errSiteTrustDraftUnavailable):
		return DenialCodeSiteTrustDraftNotAvailable
	case errors.Is(err, errSiteTrustDraftAlreadyExists):
		return DenialCodeSiteTrustDraftExists
	case errors.Is(err, errSiteInspectionContentUnsupported):
		return DenialCodeSiteInspectionUnsupportedType
	default:
		return DenialCodeExecutionFailed
	}
}

func redactSiteTrustError(err error) string {
	return err.Error()
}

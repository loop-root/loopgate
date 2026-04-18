package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/url"
	"path/filepath"
	"strings"

	"loopgate/internal/secrets"
)

const hookAuditPreviewMaxBytes = 256

func buildHookAuditProjection(req controlapipkg.HookPreValidateRequest, repoRoot string, includePreviews bool) map[string]interface{} {
	auditProjection := map[string]interface{}{
		"tool_name": req.ToolName,
	}
	if requestFingerprintSHA256, err := claudeHookApprovalRequestFingerprint(req); err == nil && strings.TrimSpace(requestFingerprintSHA256) != "" {
		auditProjection["tool_request_fingerprint_sha256"] = requestFingerprintSHA256
	}

	switch req.ToolName {
	case "Bash":
		rawCommand, _ := hookInputString(req.ToolInput, "command")
		auditProjection["tool_target_kind"] = "shell_command"
		auditProjection["command_sha256"] = sha256Hex(rawCommand)
		auditProjection["command_verb"] = firstToken(rawCommand)
		if includePreviews {
			auditProjection["command_redacted_preview"] = boundedRedactedHookPreview(rawCommand)
		}
	case "Read", "Write", "Edit", "MultiEdit":
		filePath, _ := hookInputString(req.ToolInput, "file_path")
		auditProjection["tool_target_kind"] = "filesystem_path"
		auditProjection["target_path"] = strings.TrimSpace(filePath)
		auditProjection["resolved_target_path"] = resolveHookTargetPath(filePath, req.CWD, repoRoot)
	case "Glob", "Grep":
		searchPath, _ := hookInputString(req.ToolInput, "path")
		if strings.TrimSpace(searchPath) == "" {
			searchPath = req.CWD
		}
		auditProjection["tool_target_kind"] = "filesystem_search_root"
		auditProjection["target_path"] = strings.TrimSpace(searchPath)
		auditProjection["resolved_target_path"] = resolveHookTargetPath(searchPath, req.CWD, repoRoot)
	case "WebFetch":
		requestURL, _ := hookInputString(req.ToolInput, "url")
		auditProjection["tool_target_kind"] = "http_url"
		auditProjection["url_sha256"] = sha256Hex(requestURL)
		if includePreviews {
			auditProjection["url_redacted_preview"] = boundedRedactedHookPreview(requestURL)
		}
		if parsedURL, err := url.Parse(requestURL); err == nil {
			auditProjection["request_host"] = strings.ToLower(parsedURL.Hostname())
			auditProjection["request_scheme"] = strings.ToLower(parsedURL.Scheme)
		}
	case "WebSearch":
		queryText, _ := hookInputString(req.ToolInput, "query")
		auditProjection["tool_target_kind"] = "web_search_query"
		auditProjection["query_sha256"] = sha256Hex(queryText)
		if includePreviews {
			auditProjection["query_redacted_preview"] = boundedRedactedHookPreview(queryText)
		}
	default:
		if req.ToolName != "" {
			auditProjection["tool_target_kind"] = "unknown"
		}
	}

	if strings.TrimSpace(req.CWD) != "" {
		auditProjection["hook_cwd"] = filepath.Clean(strings.TrimSpace(req.CWD))
	}
	return auditProjection
}

func mergeHookAuditProjection(auditData map[string]interface{}, req controlapipkg.HookPreValidateRequest, repoRoot string, includePreviews bool) map[string]interface{} {
	if auditData == nil {
		auditData = map[string]interface{}{}
	}
	for fieldName, fieldValue := range buildHookAuditProjection(req, repoRoot, includePreviews) {
		if _, exists := auditData[fieldName]; exists {
			continue
		}
		auditData[fieldName] = fieldValue
	}
	return auditData
}

func boundedRedactedHookPreview(rawText string) string {
	redactedText := secrets.RedactText(strings.TrimSpace(rawText))
	if len(redactedText) <= hookAuditPreviewMaxBytes {
		return redactedText
	}
	return strings.TrimSpace(redactedText[:hookAuditPreviewMaxBytes]) + "..."
}

func sha256Hex(rawText string) string {
	trimmedText := strings.TrimSpace(rawText)
	if trimmedText == "" {
		return ""
	}
	hashSum := sha256.Sum256([]byte(trimmedText))
	return hex.EncodeToString(hashSum[:])
}

func firstToken(rawText string) string {
	fields := strings.Fields(strings.TrimSpace(rawText))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func formatHookAuditActor(peerUID uint32, sessionID string) string {
	if strings.TrimSpace(sessionID) != "" {
		return fmt.Sprintf("claude_session:%s", strings.TrimSpace(sessionID))
	}
	return fmt.Sprintf("peer_uid:%d", peerUID)
}

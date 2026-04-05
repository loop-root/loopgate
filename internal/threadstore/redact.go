package threadstore

import "strings"

// redactEventData sanitizes event data before persistence as defense-in-depth.
// This ensures that even if callers forget to strip sensitive fields, the
// threadstore will not persist raw secrets, full model payloads, or verbose
// tool output that could leak credentials or tokens.
//
// The function is intentionally conservative: it only strips fields known to
// carry sensitive data, and leaves the rest of the map intact so the thread
// file remains useful for replay and debugging.
func redactEventData(eventType string, data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}

	switch eventType {
	case EventOrchModelResponse:
		return redactModelResponse(data)
	case EventOrchToolResult:
		return redactToolResult(data)
	default:
		// User messages, assistant messages, and other orchestration events
		// do not typically contain sensitive payloads. Scrub env-like values
		// from any data map as a catch-all.
		return scrubEnvLikeValues(data)
	}
}

// redactModelResponse strips the full model payload from orchestration model
// responses. The raw API response may contain system prompts, token counts,
// and other data that should not be persisted in user-visible thread files.
func redactModelResponse(data map[string]interface{}) map[string]interface{} {
	out := shallowCopyMap(data)

	// Strip raw API response body — may contain system prompts and internal state.
	delete(out, "raw_response")
	delete(out, "raw_payload")
	delete(out, "api_response")

	// Keep: model name, stop reason, usage stats, content blocks (already surfaced).
	return scrubEnvLikeValues(out)
}

// redactToolResult strips verbose tool output that may contain secrets,
// credentials, or large binary data. Preserves the tool name, status, and
// a truncated preview of the output for debugging.
func redactToolResult(data map[string]interface{}) map[string]interface{} {
	out := shallowCopyMap(data)

	// Truncate raw output to a safe preview length.
	if rawOutput, ok := out["output"].(string); ok {
		out["output"] = truncateToolOutput(rawOutput, maxToolOutputLen)
	}

	// Strip fields that may carry filesystem contents or credentials.
	delete(out, "raw_output")
	delete(out, "stderr")
	delete(out, "env")

	return scrubEnvLikeValues(out)
}

// maxToolOutputLen is the maximum length of tool output preserved in thread files.
// Longer output is truncated with a redaction notice.
const maxToolOutputLen = 2048

// truncateToolOutput truncates s to maxLen bytes, appending a redaction notice
// if truncation occurred.
func truncateToolOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n[threadstore: output truncated]"
}

// scrubEnvLikeValues scans string values in a data map for patterns that look
// like leaked secrets (API keys, tokens, passwords in env-var format) and
// replaces them with a redaction marker.
func scrubEnvLikeValues(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	out := shallowCopyMap(data)
	for key, val := range out {
		if s, ok := val.(string); ok {
			out[key] = scrubSecretPatterns(s)
		}
	}
	return out
}

// secretPrefixes are substrings that, when found in a string value, indicate
// the value likely contains a leaked credential. The entire value is replaced
// with a redaction notice rather than attempting surgery — false positives are
// preferable to leaked secrets.
var secretPrefixes = []string{
	"sk-ant-",     // Anthropic API keys
	"sk-",         // OpenAI-style API keys
	"ghp_",        // GitHub personal access tokens
	"gho_",        // GitHub OAuth tokens
	"ghs_",        // GitHub app tokens
	"glpat-",      // GitLab personal access tokens
	"xoxb-",       // Slack bot tokens
	"xoxp-",       // Slack user tokens
	"AKIA",        // AWS access key IDs
	"eyJ",         // JWT tokens (base64-encoded JSON header)
	"Bearer ",     // Authorization headers
	"Basic ",      // Basic auth headers
	"password=",   // Password in query strings
	"passwd=",     // Password in query strings
	"secret=",     // Secret in query strings
	"token=",      // Token in query strings
	"api_key=",    // API key in query strings
	"apikey=",     // API key in query strings
	"access_token=", // OAuth tokens in query strings
}

const redactedMarker = "[REDACTED]"

// scrubSecretPatterns checks if s contains any known secret pattern and
// replaces the entire string if so. This is intentionally aggressive —
// a false positive (redacting a non-secret) is far less damaging than
// a false negative (persisting a real secret).
func scrubSecretPatterns(s string) string {
	for _, prefix := range secretPrefixes {
		if strings.Contains(s, prefix) {
			return redactedMarker
		}
	}
	return s
}

func shallowCopyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

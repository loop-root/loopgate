**Last updated:** 2026-03-24

# Security Best Practices Report

Date: 2026-03-07

## Executive Summary

Morph and Loopgate already implement several strong secure-by-default controls for a local control-plane architecture: strict JSON/YAML decoding, deny-by-default policy checks, peer-bound and signed Loopgate requests, OS-backed secure secret storage on macOS, quarantined remote payloads, explicit result classification, fail-closed filesystem-root validation, fail-closed ledger distillation integrity checks, private runtime directories, and tighter PKCE redirect validation. The remaining best-practice gaps are now narrower and mostly centered on launch-bound local identity, audit tamper evidence, and provider-specific output minimization.

## Recently Addressed Findings

### SEC-BP-001: Empty `allowed_roots` broadening is now fixed

- Rule ID: `GO-CONFIG-FAIL-CLOSED`
- Severity: High
- Location: [internal/config/policy.go](../../internal/config/policy.go#L87) `applyPolicyDefaults`
- Evidence:

```go
if len(pol.Tools.Filesystem.AllowedRoots) == 0 {
	pol.Tools.Filesystem.AllowedRoots = []string{"."}
}
```

- Current state: fixed. Filesystem-enabled policy now fails closed when `allowed_roots` is empty or blank after normalization.

### SEC-BP-002: Silent malformed-ledger skipping is now fixed

- Rule ID: `GO-AUDIT-INTEGRITY`
- Severity: Medium
- Location: [internal/memory/distillate.go](../../internal/memory/distillate.go#L74) `DistillFromLedger`
- Evidence:

```go
if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
	// Skip malformed lines, but continue.
	continue
}
```

- Current state: fixed. Distillation now returns an explicit integrity error on malformed ledger lines instead of silently skipping them.

### SEC-BP-003: Sensitive runtime directory permissions are now fixed

- Rule ID: `GO-FILES-PERMISSIONS`
- Severity: Medium
- Location:
  - [internal/state/state.go](../../internal/state/state.go#L67) `Save`
  - [internal/modelruntime/runtime.go](../../internal/modelruntime/runtime.go#L185) `SaveRuntimeConfig`
- Evidence:

```go
if err := os.MkdirAll(dir, 0755); err != nil {
	return err
}
```

```go
if err := os.MkdirAll(configDir, 0o755); err != nil {
	return fmt.Errorf("create model runtime config dir: %w", err)
}
```

- Current state: fixed. Runtime state and model runtime config directories now default to `0700`.

### SEC-BP-004: Overly broad PKCE redirect acceptance is now fixed

- Rule ID: `GO-OAUTH-REDIRECT-VALIDATION`
- Severity: Medium
- Location: [internal/loopgate/integration_config.go](../../internal/loopgate/integration_config.go#L317) `parseAndValidateRedirectURL`
- Evidence:

```go
if parsedURL.Scheme == "https" {
	return parsedURL, nil
}
if parsedURL.Scheme == "http" && isLocalhostHost(parsedURL.Hostname()) {
	return parsedURL, nil
}
if parsedURL.Scheme != "" {
	return parsedURL, nil
}
```

- Current state: fixed. PKCE `redirect_url` now allows only `https` or loopback `http`.

## Positive Security Controls Already Present

- [internal/loopgate/server.go](../../internal/loopgate/server.go#L237): Loopgate uses explicit server timeouts and `MaxHeaderBytes`.
- [internal/loopgate/server.go](../../internal/loopgate/server.go#L1721): control-plane JSON bodies are size-limited with `http.MaxBytesReader` and strict decoding.
- [internal/secrets/macos_keychain.go](../../internal/secrets/macos_keychain.go#L44): macOS Keychain-backed secret storage is implemented, with secret material passed via stdin rather than CLI args.
- [internal/loopgate/quarantine.go](../../internal/loopgate/quarantine.go#L19): remote payload quarantine is real, private, and stored outside prompt-eligible paths.
- [internal/loopgateresult/render.go](../../internal/loopgateresult/render.go#L32): Morph obeys Loopgate result classification instead of inferring prompt/display behavior from content shape.

## Suggested Improvement Order

1. Strengthen launch-bound local identity for Loopgate and future `morphui` bootstrap.
2. Add stronger tamper-evidence or checkpointing for local audit/state artifacts.
3. Tighten provider-specific output schemas and sensitivity guidance for configured response fields.
4. Continue the local file/socket permission sweep across any future runtime artifacts.

## Scope Notes

- This review was grounded in the current Go backend/control-plane code and the current local-first deployment model reflected in the repository docs.
- It does not treat lack of TLS on the local Unix-socket boundary as a finding.
- It does not treat the intentionally missing Windows/Linux secure-store backends as a vulnerability, because those paths currently fail closed.

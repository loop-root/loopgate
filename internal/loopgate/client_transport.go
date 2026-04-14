package loopgate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (client *Client) doJSON(ctx context.Context, method string, path string, capabilityToken string, requestBody interface{}, responseBody interface{}, extraHeaders map[string]string) error {
	return client.doJSONWithTimeout(ctx, client.defaultRequestTimeout, method, path, capabilityToken, requestBody, responseBody, extraHeaders)
}

func (client *Client) doCapabilityJSON(ctx context.Context, requestTimeout time.Duration, method string, path string, capabilityToken string, requestBody interface{}, responseBody *CapabilityResponse, extraHeaders map[string]string) error {
	return client.doCapabilityJSONWithTimeoutRetry(ctx, requestTimeout, method, path, capabilityToken, requestBody, responseBody, extraHeaders, false)
}

func (client *Client) doJSONWithHeaders(ctx context.Context, method string, path string, capabilityToken string, requestBody interface{}, responseBody interface{}, extraHeaders map[string]string) error {
	return client.doJSONWithTimeout(ctx, client.defaultRequestTimeout, method, path, capabilityToken, requestBody, responseBody, extraHeaders)
}

func (client *Client) doJSONWithTimeout(ctx context.Context, requestTimeout time.Duration, method string, path string, capabilityToken string, requestBody interface{}, responseBody interface{}, extraHeaders map[string]string) error {
	return client.doJSONWithTimeoutRetry(ctx, requestTimeout, method, path, capabilityToken, requestBody, responseBody, extraHeaders, false)
}

func (client *Client) doJSONWithTimeoutRetry(ctx context.Context, requestTimeout time.Duration, method string, path string, capabilityToken string, requestBody interface{}, responseBody interface{}, extraHeaders map[string]string, retried bool) error {
	requestContext := ctx
	cancel := func() {}
	if _, hasDeadline := requestContext.Deadline(); !hasDeadline && requestTimeout > 0 {
		requestContext, cancel = context.WithTimeout(requestContext, requestTimeout)
	}
	defer cancel()

	var bodyBytes []byte
	if requestBody == nil {
		bodyBytes = nil
	} else {
		marshaledBytes, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyBytes = marshaledBytes
	}
	bodyReader := bytes.NewReader(bodyBytes)

	httpRequest, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(capabilityToken) != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+capabilityToken)
	}
	for headerName, headerValue := range extraHeaders {
		httpRequest.Header.Set(headerName, headerValue)
	}
	if err := client.attachRequestSignature(httpRequest, path, bodyBytes); err != nil {
		return err
	}

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("loopgate request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		var errorResponse CapabilityResponse
		if decodeErr := json.NewDecoder(httpResponse.Body).Decode(&errorResponse); decodeErr == nil && strings.TrimSpace(errorResponse.DenialReason) != "" {
			if !retried && strings.TrimSpace(capabilityToken) != "" && client.canRetryCapabilityToken(errorResponse.DenialCode) {
				if refreshedToken, retryErr := client.refreshCapabilityToken(requestContext); retryErr == nil {
					return client.doJSONWithTimeoutRetry(ctx, requestTimeout, method, path, refreshedToken, requestBody, responseBody, extraHeaders, true)
				}
			}
			return RequestDeniedError{
				DenialCode:   errorResponse.DenialCode,
				DenialReason: errorResponse.DenialReason,
			}
		}
		return fmt.Errorf("loopgate returned status %d", httpResponse.StatusCode)
	}

	if responseBody == nil {
		return nil
	}
	if err := json.NewDecoder(httpResponse.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}

func (client *Client) doCapabilityJSONWithTimeoutRetry(ctx context.Context, requestTimeout time.Duration, method string, path string, capabilityToken string, requestBody interface{}, responseBody *CapabilityResponse, extraHeaders map[string]string, retried bool) error {
	requestContext := ctx
	cancel := func() {}
	if _, hasDeadline := requestContext.Deadline(); !hasDeadline && requestTimeout > 0 {
		requestContext, cancel = context.WithTimeout(requestContext, requestTimeout)
	}
	defer cancel()

	var bodyBytes []byte
	if requestBody != nil {
		marshaledBytes, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyBytes = marshaledBytes
	}

	httpRequest, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(capabilityToken) != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+capabilityToken)
	}
	for headerName, headerValue := range extraHeaders {
		httpRequest.Header.Set(headerName, headerValue)
	}
	if err := client.attachRequestSignature(httpRequest, path, bodyBytes); err != nil {
		return err
	}

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("loopgate request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	if err := json.NewDecoder(httpResponse.Body).Decode(responseBody); err != nil {
		if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
			return fmt.Errorf("loopgate returned status %d", httpResponse.StatusCode)
		}
		return fmt.Errorf("decode response body: %w", err)
	}

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		if !retried && strings.TrimSpace(capabilityToken) != "" && client.canRetryCapabilityToken(responseBody.DenialCode) {
			if refreshedToken, retryErr := client.refreshCapabilityToken(requestContext); retryErr == nil {
				return client.doCapabilityJSONWithTimeoutRetry(ctx, requestTimeout, method, path, refreshedToken, requestBody, responseBody, extraHeaders, true)
			}
		}
		return nil
	}
	return nil
}

func (client *Client) attachRequestSignature(httpRequest *http.Request, path string, bodyBytes []byte) error {
	if strings.TrimSpace(httpRequest.Header.Get("X-Loopgate-Control-Session")) != "" ||
		strings.TrimSpace(httpRequest.Header.Get("X-Loopgate-Request-Timestamp")) != "" ||
		strings.TrimSpace(httpRequest.Header.Get("X-Loopgate-Request-Nonce")) != "" ||
		strings.TrimSpace(httpRequest.Header.Get("X-Loopgate-Request-Signature")) != "" {
		return nil
	}

	client.mu.Lock()
	controlSessionID := client.controlSessionID
	sessionMACKey := client.sessionMACKey
	client.mu.Unlock()

	if strings.TrimSpace(controlSessionID) == "" || strings.TrimSpace(sessionMACKey) == "" {
		return nil
	}

	requestNonce, err := clientRandomHex(12)
	if err != nil {
		return fmt.Errorf("generate request nonce: %w", err)
	}
	requestTimestamp := time.Now().UTC().Format(time.RFC3339Nano)
	canonicalPath := strings.TrimSpace(httpRequest.URL.Path)
	if canonicalPath == "" {
		canonicalPath = path
	}
	requestSignature := computeRequestSignature(sessionMACKey, httpRequest.Method, canonicalPath, controlSessionID, requestTimestamp, requestNonce, bodyBytes)

	httpRequest.Header.Set("X-Loopgate-Control-Session", controlSessionID)
	httpRequest.Header.Set("X-Loopgate-Request-Timestamp", requestTimestamp)
	httpRequest.Header.Set("X-Loopgate-Request-Nonce", requestNonce)
	httpRequest.Header.Set("X-Loopgate-Request-Signature", requestSignature)
	return nil
}

func computeRequestSignature(sessionMACKey string, method string, path string, controlSessionID string, requestTimestamp string, requestNonce string, bodyBytes []byte) string {
	bodyHash := sha256.Sum256(bodyBytes)
	signingPayload := strings.Join([]string{
		method,
		path,
		controlSessionID,
		requestTimestamp,
		requestNonce,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")

	mac := hmac.New(sha256.New, []byte(sessionMACKey))
	_, _ = mac.Write([]byte(signingPayload))
	return hex.EncodeToString(mac.Sum(nil))
}

func clientRandomHex(byteCount int) (string, error) {
	randomBytes := make([]byte, byteCount)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

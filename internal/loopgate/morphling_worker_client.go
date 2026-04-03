package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"morph/internal/identifiers"
)

type MorphlingWorkerSessionConfig struct {
	MorphlingID      string
	ControlSessionID string
	WorkerToken      string
	SessionMACKey    string
	ExpiresAt        time.Time
}

type MorphlingWorkerClient struct {
	socketPath string
	httpClient *http.Client
	baseURL    string

	defaultRequestTimeout time.Duration

	morphlingID      string
	controlSessionID string
	workerToken      string
	sessionMACKey    string
	expiresAt        time.Time
}

func NewMorphlingWorkerClient(socketPath string, workerSession MorphlingWorkerSessionConfig) (*MorphlingWorkerClient, error) {
	if err := validateMorphlingWorkerSessionConfig(workerSession, time.Now()); err != nil {
		return nil, err
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dialer := net.Dialer{}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &MorphlingWorkerClient{
		socketPath:            socketPath,
		httpClient:            &http.Client{Transport: transport},
		baseURL:               "http://loopgate",
		defaultRequestTimeout: 10 * time.Second,
		morphlingID:           strings.TrimSpace(workerSession.MorphlingID),
		controlSessionID:      strings.TrimSpace(workerSession.ControlSessionID),
		workerToken:           strings.TrimSpace(workerSession.WorkerToken),
		sessionMACKey:         strings.TrimSpace(workerSession.SessionMACKey),
		expiresAt:             workerSession.ExpiresAt.UTC(),
	}, nil
}

func OpenMorphlingWorkerSession(ctx context.Context, socketPath string, launchToken string) (*MorphlingWorkerClient, MorphlingWorkerSessionResponse, error) {
	operatorClient := NewClient(socketPath)
	sessionResponse, err := operatorClient.OpenMorphlingWorkerSession(ctx, MorphlingWorkerOpenRequest{
		LaunchToken: launchToken,
	})
	if err != nil {
		return nil, MorphlingWorkerSessionResponse{}, err
	}
	expiresAtUTC, err := time.Parse(time.RFC3339Nano, sessionResponse.ExpiresAtUTC)
	if err != nil {
		return nil, MorphlingWorkerSessionResponse{}, fmt.Errorf("parse morphling worker session expiry: %w", err)
	}
	workerClient, err := NewMorphlingWorkerClient(socketPath, MorphlingWorkerSessionConfig{
		MorphlingID:      sessionResponse.MorphlingID,
		ControlSessionID: sessionResponse.ControlSessionID,
		WorkerToken:      sessionResponse.WorkerToken,
		SessionMACKey:    sessionResponse.SessionMACKey,
		ExpiresAt:        expiresAtUTC,
	})
	if err != nil {
		return nil, MorphlingWorkerSessionResponse{}, err
	}
	return workerClient, sessionResponse, nil
}

func (client *Client) OpenMorphlingWorkerSession(ctx context.Context, request MorphlingWorkerOpenRequest) (MorphlingWorkerSessionResponse, error) {
	var response MorphlingWorkerSessionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/worker/open", "", request, &response, nil); err != nil {
		return MorphlingWorkerSessionResponse{}, err
	}
	return response, nil
}

func (workerClient *MorphlingWorkerClient) Start(ctx context.Context, request MorphlingWorkerStartRequest) (MorphlingWorkerActionResponse, error) {
	var response MorphlingWorkerActionResponse
	if err := workerClient.doJSON(ctx, http.MethodPost, "/v1/morphlings/worker/start", request, &response); err != nil {
		return MorphlingWorkerActionResponse{}, err
	}
	return response, nil
}

func (workerClient *MorphlingWorkerClient) Update(ctx context.Context, request MorphlingWorkerUpdateRequest) (MorphlingWorkerActionResponse, error) {
	var response MorphlingWorkerActionResponse
	if err := workerClient.doJSON(ctx, http.MethodPost, "/v1/morphlings/worker/update", request, &response); err != nil {
		return MorphlingWorkerActionResponse{}, err
	}
	return response, nil
}

func (workerClient *MorphlingWorkerClient) Complete(ctx context.Context, request MorphlingWorkerCompleteRequest) (MorphlingWorkerActionResponse, error) {
	var response MorphlingWorkerActionResponse
	if err := workerClient.doJSON(ctx, http.MethodPost, "/v1/morphlings/worker/complete", request, &response); err != nil {
		return MorphlingWorkerActionResponse{}, err
	}
	return response, nil
}

func (workerClient *MorphlingWorkerClient) doJSON(ctx context.Context, method string, path string, requestBody interface{}, responseBody interface{}) error {
	requestContext := ctx
	cancel := func() {}
	if _, hasDeadline := requestContext.Deadline(); !hasDeadline && workerClient.defaultRequestTimeout > 0 {
		requestContext, cancel = context.WithTimeout(requestContext, workerClient.defaultRequestTimeout)
	}
	defer cancel()

	if validateMorphlingWorkerSessionConfig(MorphlingWorkerSessionConfig{
		MorphlingID:      workerClient.morphlingID,
		ControlSessionID: workerClient.controlSessionID,
		WorkerToken:      workerClient.workerToken,
		SessionMACKey:    workerClient.sessionMACKey,
		ExpiresAt:        workerClient.expiresAt,
	}, time.Now()) != nil {
		return ErrDelegatedSessionRefreshRequired
	}

	var bodyBytes []byte
	if requestBody != nil {
		marshaledBytes, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal worker request body: %w", err)
		}
		bodyBytes = marshaledBytes
	}
	httpRequest, err := http.NewRequestWithContext(requestContext, method, workerClient.baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("build worker request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-Loopgate-Morphling-Worker-Token", workerClient.workerToken)
	httpRequest.Header.Set("X-Loopgate-Control-Session", workerClient.controlSessionID)
	requestNonce, err := clientRandomHex(12)
	if err != nil {
		return fmt.Errorf("generate worker request nonce: %w", err)
	}
	requestTimestamp := time.Now().UTC().Format(time.RFC3339Nano)
	httpRequest.Header.Set("X-Loopgate-Request-Timestamp", requestTimestamp)
	httpRequest.Header.Set("X-Loopgate-Request-Nonce", requestNonce)
	httpRequest.Header.Set("X-Loopgate-Request-Signature", computeRequestSignature(workerClient.sessionMACKey, method, path, workerClient.controlSessionID, requestTimestamp, requestNonce, bodyBytes))

	httpResponse, err := workerClient.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("loopgate worker request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		var errorResponse CapabilityResponse
		if decodeErr := json.NewDecoder(httpResponse.Body).Decode(&errorResponse); decodeErr == nil && strings.TrimSpace(errorResponse.DenialReason) != "" {
			if strings.TrimSpace(errorResponse.DenialCode) != "" {
				return fmt.Errorf("loopgate denied request (%s): %s", errorResponse.DenialCode, errorResponse.DenialReason)
			}
			return fmt.Errorf("loopgate denied request: %s", errorResponse.DenialReason)
		}
		return fmt.Errorf("loopgate returned status %d", httpResponse.StatusCode)
	}

	if responseBody == nil {
		return nil
	}
	if err := json.NewDecoder(httpResponse.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode worker response body: %w", err)
	}
	return nil
}

func validateMorphlingWorkerSessionConfig(workerSession MorphlingWorkerSessionConfig, now time.Time) error {
	if err := identifiers.ValidateSafeIdentifier("morphling_id", strings.TrimSpace(workerSession.MorphlingID)); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("worker control session id", strings.TrimSpace(workerSession.ControlSessionID)); err != nil {
		return err
	}
	if strings.TrimSpace(workerSession.WorkerToken) == "" {
		return fmt.Errorf("missing morphling worker token")
	}
	if strings.TrimSpace(workerSession.SessionMACKey) == "" {
		return fmt.Errorf("missing morphling worker session mac key")
	}
	if workerSession.ExpiresAt.IsZero() {
		return fmt.Errorf("missing morphling worker session expiry")
	}
	if EvaluateDelegatedSessionState(now, workerSession.ExpiresAt) == DelegatedSessionStateRefreshRequired {
		return ErrDelegatedSessionRefreshRequired
	}
	return nil
}

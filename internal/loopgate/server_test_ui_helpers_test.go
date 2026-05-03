package loopgate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
	"testing"
	"time"
)

func readUIReplayEvents(t *testing.T, client *Client, lastEventID string) []controlapipkg.UIEventEnvelope {
	t.Helper()

	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token for ui events: %v", err)
	}

	requestContext, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, client.baseURL+"/v1/ui/events", nil)
	if err != nil {
		t.Fatalf("build ui events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+capabilityToken)
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	if err := client.attachRequestSignature(request, "/v1/ui/events", nil); err != nil {
		t.Fatalf("attach ui events signature: %v", err)
	}

	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("do ui events request: %v", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected ui events status: %d", httpResponse.StatusCode)
	}

	reader := bufio.NewReader(httpResponse.Body)
	events := make([]controlapipkg.UIEventEnvelope, 0, 8)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var uiEvent controlapipkg.UIEventEnvelope
		if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "data: ")), &uiEvent); err != nil {
			t.Fatalf("decode ui event: %v", err)
		}
		events = append(events, uiEvent)
	}
	return events
}

func readUIRecentEvents(t *testing.T, client *Client, lastEventID string) []controlapipkg.UIEventEnvelope {
	t.Helper()

	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token for recent ui events: %v", err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, client.baseURL+"/v1/ui/events/recent", nil)
	if err != nil {
		t.Fatalf("build recent ui events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+capabilityToken)
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	if err := client.attachRequestSignature(request, "/v1/ui/events/recent", nil); err != nil {
		t.Fatalf("attach recent ui events signature: %v", err)
	}

	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("do recent ui events request: %v", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected recent ui events status: %d", httpResponse.StatusCode)
	}

	var response controlapipkg.UIRecentEventsResponse
	if err := json.NewDecoder(httpResponse.Body).Decode(&response); err != nil {
		t.Fatalf("decode recent ui events response: %v", err)
	}
	return response.Events
}

func containsUIEventType(events []controlapipkg.UIEventEnvelope, expectedType string) bool {
	for _, uiEvent := range events {
		if uiEvent.Type == expectedType {
			return true
		}
	}
	return false
}

func containsUICapabilityEvent(events []controlapipkg.UIEventEnvelope, capability string) bool {
	for _, uiEvent := range events {
		encodedEvent, err := json.Marshal(uiEvent)
		if err != nil {
			continue
		}
		if strings.Contains(string(encodedEvent), fmt.Sprintf("\"capability\":\"%s\"", capability)) {
			return true
		}
	}
	return false
}

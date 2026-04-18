package loopgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
)

func (server *Server) decodeJSONBody(writer http.ResponseWriter, request *http.Request, maxBodyBytes int64, destination interface{}) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain a single JSON object")
	}
	return nil
}

func (server *Server) readAndVerifySignedBody(writer http.ResponseWriter, request *http.Request, maxBodyBytes int64, controlSessionID string) ([]byte, controlapipkg.CapabilityResponse, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBodyBytes)
	requestBodyBytes, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: fmt.Sprintf("invalid request body: %v", err),
			DenialCode:   controlapipkg.DenialCodeMalformedRequest,
		}, false
	}
	if verificationResponse, ok := server.verifySignedRequest(request, requestBodyBytes, controlSessionID); !ok {
		return nil, verificationResponse, false
	}
	return requestBodyBytes, controlapipkg.CapabilityResponse{}, true
}

func decodeJSONBytes(requestBodyBytes []byte, destination interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(requestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain a single JSON object")
	}
	return nil
}

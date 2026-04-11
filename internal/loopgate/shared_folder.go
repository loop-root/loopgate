package loopgate

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

const (
	defaultSharedFolderName        = "Shared with Morph"
	defaultSharedFolderSandboxName = "shared"
)

func (server *Server) handleSharedFolderStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityFolderAccessRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	statusResponse, err := server.sharedFolderStatus()
	if err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, statusResponse)
}

func (server *Server) handleSharedFolderSync(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityFolderAccessWrite) {
		return
	}
	requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	if len(requestBodyBytes) > 0 {
		var emptyRequest struct{}
		if err := decodeJSONBytes(requestBodyBytes, &emptyRequest); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
	}

	statusResponse, err := server.syncDefaultSharedFolder(tokenClaims)
	if err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, statusResponse)
}

func (server *Server) syncDefaultSharedFolder(tokenClaims capabilityToken) (SharedFolderStatusResponse, error) {
	preset, _ := folderAccessPresetByID(folderAccessSharedID)
	status, err := server.syncFolderAccessPreset(preset, true, tokenClaims)
	if err != nil {
		return SharedFolderStatusResponse{}, err
	}
	return SharedFolderStatusResponse{
		Name:                status.Name,
		HostPath:            status.HostPath,
		SandboxRelativePath: status.SandboxRelativePath,
		SandboxAbsolutePath: status.SandboxAbsolutePath,
		HostExists:          status.HostExists,
		MirrorReady:         status.MirrorReady,
		EntryCount:          status.EntryCount,
	}, nil
}

func (server *Server) sharedFolderStatus() (SharedFolderStatusResponse, error) {
	preset, _ := folderAccessPresetByID(folderAccessSharedID)
	status, err := server.folderAccessStatusForPreset(preset, true)
	if err != nil {
		return SharedFolderStatusResponse{}, err
	}
	return SharedFolderStatusResponse{
		Name:                status.Name,
		HostPath:            status.HostPath,
		SandboxRelativePath: status.SandboxRelativePath,
		SandboxAbsolutePath: status.SandboxAbsolutePath,
		HostExists:          status.HostExists,
		MirrorReady:         status.MirrorReady,
		EntryCount:          status.EntryCount,
	}, nil
}

func (server *Server) defaultSharedFolderPath() (string, error) {
	if server.resolveUserHomeDir == nil {
		return "", fmt.Errorf("resolve user home is unavailable")
	}
	userHomeDirectory, err := server.resolveUserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(userHomeDirectory, defaultSharedFolderName), nil
}

func countDirectoryEntries(directoryPath string) int {
	directoryEntries, err := os.ReadDir(directoryPath)
	if err != nil {
		return 0
	}
	return len(directoryEntries)
}

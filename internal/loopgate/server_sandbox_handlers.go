package loopgate

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"morph/internal/sandbox"
)

var errSandboxHostDestinationInvalid = errors.New("sandbox host destination is invalid")
var errSandboxHostPathUnbound = errors.New("sandbox host path is outside session operator mounts")
var errSandboxHostWriteGrantRequired = errors.New("sandbox export requires an operator mount write grant")
var errSandboxAuditUnavailable = errors.New("sandbox audit is unavailable")
var errSandboxArtifactNotStaged = errors.New("sandbox output is not staged")

func (server *Server) handleSandboxImport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_write") {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var importRequest SandboxImportRequest
	if err := decodeJSONBytes(requestBodyBytes, &importRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := importRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	importResponse, err := server.importIntoSandbox(tokenClaims, importRequest)
	if err != nil {
		server.writeJSON(writer, sandboxHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactSandboxError(err),
			DenialCode:   sandboxDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, importResponse)
}

func (server *Server) handleSandboxStage(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_write") {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var stageRequest SandboxStageRequest
	if err := decodeJSONBytes(requestBodyBytes, &stageRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := stageRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	stageResponse, err := server.stageSandboxArtifact(tokenClaims, stageRequest)
	if err != nil {
		server.writeJSON(writer, sandboxHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactSandboxError(err),
			DenialCode:   sandboxDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, stageResponse)
}

func (server *Server) handleSandboxMetadata(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_read") {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var metadataRequest SandboxMetadataRequest
	if err := decodeJSONBytes(requestBodyBytes, &metadataRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := metadataRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	metadataResponse, err := server.stageArtifactMetadata(metadataRequest.SandboxSourcePath)
	if err != nil {
		server.writeJSON(writer, sandboxHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactSandboxError(err),
			DenialCode:   sandboxDenialCode(err),
			Redacted:     true,
		})
		return
	}
	if err := server.logEvent("sandbox.metadata_viewed", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id":    tokenClaims.ControlSessionID,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
		"sandbox_relative_path": metadataResponse.SandboxRelativePath,
		"artifact_ref":          metadataResponse.ArtifactRef,
		"content_sha256":        metadataResponse.ContentSHA256,
		"size_bytes":            metadataResponse.SizeBytes,
	}); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, metadataResponse)
}

func (server *Server) handleSandboxExport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_write") {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var exportRequest SandboxExportRequest
	if err := decodeJSONBytes(requestBodyBytes, &exportRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := exportRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	exportResponse, err := server.exportSandboxArtifact(tokenClaims, exportRequest)
	if err != nil {
		server.writeJSON(writer, sandboxHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactSandboxError(err),
			DenialCode:   sandboxDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, exportResponse)
}

func (server *Server) handleSandboxList(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_list") {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var listRequest SandboxListRequest
	if err := decodeJSONBytes(requestBodyBytes, &listRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := listRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	listResponse, err := server.listSandboxDirectory(tokenClaims, listRequest)
	if err != nil {
		server.writeJSON(writer, sandboxHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactSandboxError(err),
			DenialCode:   sandboxDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, listResponse)
}

func (server *Server) listSandboxDirectory(tokenClaims capabilityToken, listRequest SandboxListRequest) (SandboxListResponse, error) {
	if err := server.sandboxPaths.Ensure(); err != nil {
		return SandboxListResponse{}, fmt.Errorf("ensure sandbox paths: %w", err)
	}

	resolvedPath, relativePath, err := server.sandboxPaths.ResolveHomePath(listRequest.SandboxPath)
	if err != nil {
		return SandboxListResponse{}, err
	}

	dirEntries, err := os.ReadDir(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return SandboxListResponse{}, sandbox.ErrSandboxSourceUnavailable
		}
		return SandboxListResponse{}, fmt.Errorf("read sandbox directory: %w", err)
	}

	entries := make([]SandboxListEntry, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		if dirEntry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := dirEntry.Info()
		if err != nil {
			continue
		}
		entryType := "file"
		if dirEntry.IsDir() {
			entryType = "directory"
		}
		entries = append(entries, SandboxListEntry{
			Name:       dirEntry.Name(),
			EntryType:  entryType,
			SizeBytes:  info.Size(),
			ModTimeUTC: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	if err := server.logEvent("sandbox.listed", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"sandbox_path":         relativePath,
	}); err != nil {
		return SandboxListResponse{}, fmt.Errorf("%w: %v", errSandboxAuditUnavailable, err)
	}

	return SandboxListResponse{
		SandboxPath:         relativePath,
		SandboxAbsolutePath: sandbox.VirtualizeRelativeHomePath(relativePath),
		Entries:             entries,
	}, nil
}

func (server *Server) importIntoSandbox(tokenClaims capabilityToken, importRequest SandboxImportRequest) (SandboxOperationResponse, error) {
	if err := server.sandboxPaths.Ensure(); err != nil {
		return SandboxOperationResponse{}, fmt.Errorf("ensure sandbox paths: %w", err)
	}
	resolvedSourcePath, _, err := sandbox.ResolveHostSource(importRequest.HostSourcePath)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	if _, err := operatorMountRootForResolvedHostPath(server, tokenClaims.ControlSessionID, resolvedSourcePath); err != nil {
		return SandboxOperationResponse{}, fmt.Errorf("%w: %v", errSandboxHostPathUnbound, err)
	}
	destinationAbsolutePath, destinationRelativePath, err := server.sandboxPaths.BuildImportDestination(importRequest.DestinationName)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	entryType, err := sandbox.CopyPathAtomic(resolvedSourcePath, destinationAbsolutePath)
	if err != nil {
		return SandboxOperationResponse{}, err
	}

	importResponse := SandboxOperationResponse{
		Action:              "import",
		EntryType:           entryType,
		SandboxRelativePath: destinationRelativePath,
		SandboxAbsolutePath: sandbox.VirtualizeRelativeHomePath(destinationRelativePath),
		HostPath:            resolvedSourcePath,
		SandboxRoot:         sandbox.VirtualHome,
	}
	if err := server.logEvent("sandbox.imported", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id":    tokenClaims.ControlSessionID,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
		"host_source_path":      resolvedSourcePath,
		"sandbox_relative_path": destinationRelativePath,
		"entry_type":            entryType,
	}); err != nil {
		_ = os.RemoveAll(destinationAbsolutePath)
		return SandboxOperationResponse{}, fmt.Errorf("%w: %v", errSandboxAuditUnavailable, err)
	}
	return importResponse, nil
}

func (server *Server) stageSandboxArtifact(tokenClaims capabilityToken, stageRequest SandboxStageRequest) (SandboxOperationResponse, error) {
	if err := server.sandboxPaths.Ensure(); err != nil {
		return SandboxOperationResponse{}, fmt.Errorf("ensure sandbox paths: %w", err)
	}
	resolvedSourcePath, sourceRelativePath, err := server.sandboxPaths.ResolveHomePath(stageRequest.SandboxSourcePath)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	destinationAbsolutePath, destinationRelativePath, err := server.sandboxPaths.BuildStagedOutput(stageRequest.OutputName)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	entryType, err := sandbox.CopyPathAtomic(resolvedSourcePath, destinationAbsolutePath)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	stagedArtifactRecord, err := server.writeStagedArtifact(resolvedSourcePath, sourceRelativePath, destinationAbsolutePath, destinationRelativePath, entryType)
	if err != nil {
		_ = os.RemoveAll(destinationAbsolutePath)
		return SandboxOperationResponse{}, err
	}

	stageResponse := SandboxOperationResponse{
		Action:              "stage",
		EntryType:           entryType,
		SandboxRelativePath: destinationRelativePath,
		SandboxAbsolutePath: sandbox.VirtualizeRelativeHomePath(destinationRelativePath),
		SourceSandboxPath:   sandbox.VirtualizeRelativeHomePath(sourceRelativePath),
		SandboxRoot:         sandbox.VirtualHome,
		ArtifactRef:         stagedArtifactRef(stagedArtifactRecord.ArtifactID),
		ContentSHA256:       stagedArtifactRecord.ContentSHA256,
		SizeBytes:           stagedArtifactRecord.SizeBytes,
	}
	if err := server.logEvent("sandbox.staged", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id":    tokenClaims.ControlSessionID,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
		"sandbox_source_path":   sourceRelativePath,
		"sandbox_relative_path": destinationRelativePath,
		"entry_type":            entryType,
		"artifact_ref":          stageResponse.ArtifactRef,
		"content_sha256":        stageResponse.ContentSHA256,
		"size_bytes":            stageResponse.SizeBytes,
	}); err != nil {
		_ = os.RemoveAll(destinationAbsolutePath)
		_ = os.Remove(recordPathForSandboxArtifact(server.repoRoot, destinationRelativePath))
		return SandboxOperationResponse{}, fmt.Errorf("%w: %v", errSandboxAuditUnavailable, err)
	}
	return stageResponse, nil
}

func (server *Server) exportSandboxArtifact(tokenClaims capabilityToken, exportRequest SandboxExportRequest) (SandboxOperationResponse, error) {
	resolvedSourcePath, sourceRelativePath, err := server.sandboxPaths.ResolveOutputsPath(exportRequest.SandboxSourcePath)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	stagedArtifactRecord, err := loadStagedArtifactRecord(server.repoRoot, sourceRelativePath)
	if err != nil {
		return SandboxOperationResponse{}, fmt.Errorf("%w: %v", errSandboxArtifactNotStaged, err)
	}
	resolvedDestinationPath, err := sandbox.ResolveHostDestination(exportRequest.HostDestinationPath)
	if err != nil {
		return SandboxOperationResponse{}, fmt.Errorf("%w: %v", errSandboxHostDestinationInvalid, err)
	}
	matchedRootPath, err := operatorMountRootForResolvedHostPath(server, tokenClaims.ControlSessionID, resolvedDestinationPath)
	if err != nil {
		return SandboxOperationResponse{}, fmt.Errorf("%w: %v", errSandboxHostPathUnbound, err)
	}
	if _, granted, err := operatorMountWriteGrantForRoot(server, tokenClaims.ControlSessionID, matchedRootPath); err != nil {
		return SandboxOperationResponse{}, err
	} else if !granted {
		return SandboxOperationResponse{}, fmt.Errorf("%w: %s", errSandboxHostWriteGrantRequired, matchedRootPath)
	}
	entryType, err := sandbox.CopyPathAtomic(resolvedSourcePath, resolvedDestinationPath)
	if err != nil {
		return SandboxOperationResponse{}, err
	}

	exportResponse := SandboxOperationResponse{
		Action:              "export",
		EntryType:           entryType,
		SandboxRelativePath: sourceRelativePath,
		SandboxAbsolutePath: sandbox.VirtualizeRelativeHomePath(sourceRelativePath),
		SourceSandboxPath:   sandbox.VirtualizeRelativeHomePath(sourceRelativePath),
		HostPath:            resolvedDestinationPath,
		SandboxRoot:         sandbox.VirtualHome,
		ArtifactRef:         stagedArtifactRef(stagedArtifactRecord.ArtifactID),
		ContentSHA256:       stagedArtifactRecord.ContentSHA256,
		SizeBytes:           stagedArtifactRecord.SizeBytes,
	}
	if err := server.logEvent("sandbox.exported", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id":    tokenClaims.ControlSessionID,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
		"sandbox_source_path":   sourceRelativePath,
		"host_destination_path": resolvedDestinationPath,
		"entry_type":            entryType,
		"artifact_ref":          exportResponse.ArtifactRef,
		"content_sha256":        exportResponse.ContentSHA256,
		"size_bytes":            exportResponse.SizeBytes,
	}); err != nil {
		_ = os.RemoveAll(resolvedDestinationPath)
		return SandboxOperationResponse{}, fmt.Errorf("%w: %v", errSandboxAuditUnavailable, err)
	}
	return exportResponse, nil
}

func sandboxHTTPStatus(err error) int {
	switch {
	case errors.Is(err, sandbox.ErrSandboxSourceUnavailable):
		return http.StatusNotFound
	case errors.Is(err, errSandboxArtifactNotStaged):
		return http.StatusNotFound
	case errors.Is(err, sandbox.ErrSandboxDestinationExists):
		return http.StatusConflict
	case errors.Is(err, sandbox.ErrSandboxPathInvalid),
		errors.Is(err, sandbox.ErrSandboxPathOutsideRoot),
		errors.Is(err, sandbox.ErrSymlinkNotAllowed),
		errors.Is(err, errSandboxHostDestinationInvalid):
		return http.StatusBadRequest
	case errors.Is(err, errSandboxHostPathUnbound),
		errors.Is(err, errSandboxHostWriteGrantRequired):
		return http.StatusForbidden
	case errors.Is(err, errSandboxAuditUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func sandboxDenialCode(err error) string {
	switch {
	case errors.Is(err, sandbox.ErrSandboxSourceUnavailable):
		return DenialCodeSandboxSourceUnavailable
	case errors.Is(err, errSandboxArtifactNotStaged):
		return DenialCodeSandboxArtifactNotStaged
	case errors.Is(err, sandbox.ErrSandboxDestinationExists):
		return DenialCodeSandboxDestinationExists
	case errors.Is(err, errSandboxHostDestinationInvalid):
		return DenialCodeSandboxHostDestinationInvalid
	case errors.Is(err, errSandboxHostPathUnbound):
		return DenialCodeControlSessionBindingInvalid
	case errors.Is(err, sandbox.ErrSymlinkNotAllowed):
		return DenialCodeSandboxSymlinkNotAllowed
	case errors.Is(err, errSandboxHostWriteGrantRequired):
		return DenialCodeApprovalRequired
	case errors.Is(err, errSandboxAuditUnavailable):
		return DenialCodeAuditUnavailable
	case errors.Is(err, sandbox.ErrSandboxPathInvalid), errors.Is(err, sandbox.ErrSandboxPathOutsideRoot):
		return DenialCodeSandboxPathInvalid
	default:
		return DenialCodeExecutionFailed
	}
}

func redactSandboxError(err error) string {
	// Return only stable sentinel messages — wrapped errors may embed host paths.
	switch {
	case errors.Is(err, sandbox.ErrSandboxSourceUnavailable):
		return sandbox.ErrSandboxSourceUnavailable.Error()
	case errors.Is(err, errSandboxArtifactNotStaged):
		return errSandboxArtifactNotStaged.Error()
	case errors.Is(err, sandbox.ErrSandboxDestinationExists):
		return sandbox.ErrSandboxDestinationExists.Error()
	case errors.Is(err, errSandboxHostDestinationInvalid):
		return errSandboxHostDestinationInvalid.Error()
	case errors.Is(err, errSandboxHostPathUnbound):
		return errSandboxHostPathUnbound.Error()
	case errors.Is(err, errSandboxHostWriteGrantRequired):
		return errSandboxHostWriteGrantRequired.Error()
	case errors.Is(err, sandbox.ErrSymlinkNotAllowed):
		return sandbox.ErrSymlinkNotAllowed.Error()
	case errors.Is(err, sandbox.ErrSandboxPathInvalid):
		return sandbox.ErrSandboxPathInvalid.Error()
	case errors.Is(err, sandbox.ErrSandboxPathOutsideRoot):
		return sandbox.ErrSandboxPathOutsideRoot.Error()
	default:
		return "sandbox operation failed"
	}
}

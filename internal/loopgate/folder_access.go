package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"morph/internal/config"
	"morph/internal/sandbox"
)

const (
	folderAccessConfigSection = "folder_access"
	folderAccessConfigVersion = "1"
	folderAccessStateSection  = "folder_access_state"
	folderAccessStateVersion  = "1"

	folderAccessSharedID    = "shared"
	folderAccessDownloadsID = "downloads"
	folderAccessDesktopID   = "desktop"
	folderAccessDocumentsID = "documents"
)

type folderAccessPreset struct {
	ID              string
	Name            string
	Description     string
	Warning         string
	SandboxName     string
	HomeRelativeDir string
	Recommended     bool
	AlwaysGranted   bool
	CreateIfMissing bool
	// HostAccessOnly marks folders accessed directly via host.folder.* capabilities.
	// No sandbox mirror is created or maintained; grant tracking still applies.
	HostAccessOnly bool
}

type folderAccessConfigFile struct {
	Version    string   `json:"version"`
	GrantedIDs []string `json:"granted_ids"`
}

type folderAccessStateFile struct {
	Version            string            `json:"version"`
	SourceFingerprints map[string]string `json:"source_fingerprints,omitempty"`
}

func defaultFolderAccessPresets() []folderAccessPreset {
	return []folderAccessPreset{
		{
			ID:              folderAccessSharedID,
			Name:            defaultSharedFolderName,
			Description:     "A low-friction tray for files you intentionally share with Morph.",
			SandboxName:     defaultSharedFolderSandboxName,
			AlwaysGranted:   true,
			Recommended:     true,
			CreateIfMissing: true,
		},
		{
			ID:              folderAccessDownloadsID,
			Name:            "Downloads",
			Description:     "Let Morph act directly on your Downloads folder on your Mac.",
			Warning:         "Morph reads and organizes files in place on your real Mac filesystem. Higher-risk writes still require Loopgate approval.",
			HomeRelativeDir: "Downloads",
			Recommended:     true,
			HostAccessOnly:  true,
		},
		{
			ID:              folderAccessDesktopID,
			Name:            "Desktop",
			Description:     "Let Morph act directly on your Desktop on your Mac.",
			Warning:         "Desktop often contains transient or personal items. Higher-risk writes still require Loopgate approval.",
			HomeRelativeDir: "Desktop",
			HostAccessOnly:  true,
		},
		{
			ID:              folderAccessDocumentsID,
			Name:            "Documents",
			Description:     "Let Morph act directly on your Documents folder on your Mac.",
			Warning:         "Documents can contain sensitive material. Higher-risk writes still require Loopgate approval.",
			HomeRelativeDir: "Documents",
			HostAccessOnly:  true,
		},
	}
}

func defaultFolderAccessConfig() folderAccessConfigFile {
	return folderAccessConfigFile{
		Version:    folderAccessConfigVersion,
		GrantedIDs: []string{folderAccessSharedID},
	}
}

func defaultFolderAccessState() folderAccessStateFile {
	return folderAccessStateFile{
		Version:            folderAccessStateVersion,
		SourceFingerprints: make(map[string]string),
	}
}

func (server *Server) handleFolderAccessSync(writer http.ResponseWriter, request *http.Request) {
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

	syncResponse, err := server.syncGrantedFolderAccess(tokenClaims)
	if err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, syncResponse)
}

func (server *Server) handleFolderAccess(writer http.ResponseWriter, request *http.Request) {
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}

	switch request.Method {
	case http.MethodGet:
		if !server.requireControlCapability(writer, tokenClaims, controlCapabilityFolderAccessRead) {
			return
		}
		if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
			server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		statusResponse, err := server.folderAccessStatus()
		if err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeExecutionFailed,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, statusResponse)
	case http.MethodPut:
		if !server.requireControlCapability(writer, tokenClaims, controlCapabilityFolderAccessWrite) {
			return
		}
		requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
		if !verified {
			server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		var updateRequest FolderAccessUpdateRequest
		if err := decodeJSONBytes(requestBodyBytes, &updateRequest); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		if err := updateRequest.Validate(); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		statusResponse, err := server.updateFolderAccess(tokenClaims, updateRequest)
		if err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeExecutionFailed,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, statusResponse)
	default:
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (server *Server) FolderAccessStatus() (FolderAccessStatusResponse, error) {
	return server.folderAccessStatus()
}

func (server *Server) folderAccessStatus() (FolderAccessStatusResponse, error) {
	grantedSet, err := server.loadFolderAccessGrantedSet()
	if err != nil {
		return FolderAccessStatusResponse{}, err
	}

	statuses := make([]FolderAccessStatus, 0, len(defaultFolderAccessPresets()))
	for _, preset := range defaultFolderAccessPresets() {
		status, err := server.folderAccessStatusForPreset(preset, grantedSet[preset.ID])
		if err != nil {
			return FolderAccessStatusResponse{}, err
		}
		statuses = append(statuses, status)
	}
	return FolderAccessStatusResponse{Folders: statuses}, nil
}

func (server *Server) syncGrantedFolderAccess(tokenClaims capabilityToken) (FolderAccessSyncResponse, error) {
	grantedSet, err := server.loadFolderAccessGrantedSet()
	if err != nil {
		return FolderAccessSyncResponse{}, err
	}
	stateFile, err := server.loadFolderAccessState()
	if err != nil {
		return FolderAccessSyncResponse{}, err
	}
	if stateFile.SourceFingerprints == nil {
		stateFile.SourceFingerprints = make(map[string]string)
	}

	statuses := make([]FolderAccessStatus, 0, len(defaultFolderAccessPresets()))
	changedIDs := make([]string, 0, len(defaultFolderAccessPresets()))
	stateChanged := false

	for _, preset := range defaultFolderAccessPresets() {
		granted := grantedSet[preset.ID]
		if !granted {
			if _, found := stateFile.SourceFingerprints[preset.ID]; found {
				delete(stateFile.SourceFingerprints, preset.ID)
				stateChanged = true
			}
			status, statusErr := server.folderAccessStatusForPreset(preset, false)
			if statusErr != nil {
				return FolderAccessSyncResponse{}, statusErr
			}
			statuses = append(statuses, status)
			continue
		}

		status, changed, sourceFingerprint, syncErr := server.syncFolderAccessPresetIfNeeded(preset, tokenClaims, stateFile.SourceFingerprints[preset.ID])
		if syncErr != nil {
			return FolderAccessSyncResponse{}, syncErr
		}
		statuses = append(statuses, status)
		if sourceFingerprint == "" {
			if _, found := stateFile.SourceFingerprints[preset.ID]; found {
				delete(stateFile.SourceFingerprints, preset.ID)
				stateChanged = true
			}
		} else if stateFile.SourceFingerprints[preset.ID] != sourceFingerprint {
			stateFile.SourceFingerprints[preset.ID] = sourceFingerprint
			stateChanged = true
		}
		if changed {
			changedIDs = append(changedIDs, preset.ID)
		}
	}

	if stateChanged {
		if err := server.saveFolderAccessState(stateFile); err != nil {
			return FolderAccessSyncResponse{}, err
		}
	}

	return FolderAccessSyncResponse{
		Folders:    statuses,
		ChangedIDs: changedIDs,
	}, nil
}

func (server *Server) updateFolderAccess(tokenClaims capabilityToken, updateRequest FolderAccessUpdateRequest) (FolderAccessStatusResponse, error) {
	normalizedGrantedIDs, err := normalizeFolderAccessGrantedIDs(updateRequest.GrantedIDs)
	if err != nil {
		return FolderAccessStatusResponse{}, err
	}
	if err := config.SaveJSONConfig(server.configStateDir, folderAccessConfigSection, folderAccessConfigFile{
		Version:    folderAccessConfigVersion,
		GrantedIDs: normalizedGrantedIDs,
	}); err != nil {
		return FolderAccessStatusResponse{}, fmt.Errorf("save folder access config: %w", err)
	}
	if err := server.applyFolderAccessSelection(tokenClaims, normalizedGrantedIDs); err != nil {
		return FolderAccessStatusResponse{}, err
	}
	return server.folderAccessStatus()
}

func normalizeFolderAccessGrantedIDs(rawGrantedIDs []string) ([]string, error) {
	presetByID := make(map[string]folderAccessPreset, len(defaultFolderAccessPresets()))
	for _, preset := range defaultFolderAccessPresets() {
		presetByID[preset.ID] = preset
	}

	grantedSet := map[string]struct{}{folderAccessSharedID: {}}
	for _, rawGrantedID := range rawGrantedIDs {
		grantedID := filepath.Clean(rawGrantedID)
		if grantedID == "." {
			grantedID = ""
		}
		preset, found := presetByID[grantedID]
		if !found {
			return nil, fmt.Errorf("unknown folder access id %q", rawGrantedID)
		}
		grantedSet[preset.ID] = struct{}{}
	}

	normalizedGrantedIDs := make([]string, 0, len(grantedSet))
	for grantedID := range grantedSet {
		normalizedGrantedIDs = append(normalizedGrantedIDs, grantedID)
	}
	sort.Strings(normalizedGrantedIDs)
	return normalizedGrantedIDs, nil
}

func (server *Server) loadFolderAccessGrantedSet() (map[string]bool, error) {
	configFile, err := config.LoadOrSeed[folderAccessConfigFile](server.configStateDir, folderAccessConfigSection, "", nil, defaultFolderAccessConfig)
	if err != nil {
		return nil, fmt.Errorf("load folder access config: %w", err)
	}
	grantedSet := make(map[string]bool, len(configFile.GrantedIDs)+1)
	grantedSet[folderAccessSharedID] = true
	for _, grantedID := range configFile.GrantedIDs {
		grantedSet[grantedID] = true
	}
	return grantedSet, nil
}

func (server *Server) loadFolderAccessState() (folderAccessStateFile, error) {
	stateFile, err := config.LoadOrSeed[folderAccessStateFile](server.configStateDir, folderAccessStateSection, "", nil, defaultFolderAccessState)
	if err != nil {
		return folderAccessStateFile{}, fmt.Errorf("load folder access state: %w", err)
	}
	if stateFile.SourceFingerprints == nil {
		stateFile.SourceFingerprints = make(map[string]string)
	}
	return stateFile, nil
}

func (server *Server) saveFolderAccessState(stateFile folderAccessStateFile) error {
	if stateFile.SourceFingerprints == nil {
		stateFile.SourceFingerprints = make(map[string]string)
	}
	if err := config.SaveJSONConfig(server.configStateDir, folderAccessStateSection, stateFile); err != nil {
		return fmt.Errorf("save folder access state: %w", err)
	}
	return nil
}

func (server *Server) applyFolderAccessSelection(tokenClaims capabilityToken, grantedIDs []string) error {
	grantedSet := make(map[string]struct{}, len(grantedIDs))
	for _, grantedID := range grantedIDs {
		grantedSet[grantedID] = struct{}{}
	}
	stateFile, err := server.loadFolderAccessState()
	if err != nil {
		return err
	}
	if stateFile.SourceFingerprints == nil {
		stateFile.SourceFingerprints = make(map[string]string)
	}

	for _, preset := range defaultFolderAccessPresets() {
		if _, granted := grantedSet[preset.ID]; granted {
			status, err := server.syncFolderAccessPreset(preset, true, tokenClaims)
			if err != nil {
				return err
			}
			if status.HostExists {
				sourceFingerprint, fingerprintErr := folderAccessSourceFingerprint(status.HostPath)
				if fingerprintErr != nil {
					return fingerprintErr
				}
				stateFile.SourceFingerprints[preset.ID] = sourceFingerprint
			}
			continue
		}
		if err := server.removeFolderAccessMirror(preset, tokenClaims); err != nil {
			return err
		}
		delete(stateFile.SourceFingerprints, preset.ID)
	}
	if err := server.saveFolderAccessState(stateFile); err != nil {
		return err
	}
	return nil
}

func (server *Server) folderAccessStatusForPreset(preset folderAccessPreset, granted bool) (FolderAccessStatus, error) {
	hostPath, err := server.folderAccessPresetHostPath(preset)
	if err != nil {
		return FolderAccessStatus{}, err
	}

	hostExists := false
	if hostInfo, statErr := os.Stat(hostPath); statErr == nil && hostInfo.IsDir() {
		hostExists = true
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return FolderAccessStatus{}, fmt.Errorf("stat %s folder: %w", preset.ID, statErr)
	}

	// HostAccessOnly folders use host.folder.* capabilities directly — no sandbox
	// mirror is created. MirrorReady signals the folder is usable when granted.
	if preset.HostAccessOnly {
		entryCount := 0
		if hostExists {
			entryCount = countDirectoryEntries(hostPath)
		}
		return FolderAccessStatus{
			ID:             preset.ID,
			Name:           preset.Name,
			Description:    preset.Description,
			Warning:        preset.Warning,
			Recommended:    preset.Recommended,
			AlwaysGranted:  preset.AlwaysGranted,
			Granted:        granted,
			HostPath:       hostPath,
			HostExists:     hostExists,
			MirrorReady:    granted && hostExists,
			EntryCount:     entryCount,
			HostAccessOnly: true,
		}, nil
	}

	if err := server.sandboxPaths.Ensure(); err != nil {
		return FolderAccessStatus{}, fmt.Errorf("ensure sandbox paths: %w", err)
	}
	destinationAbsolutePath, destinationRelativePath, err := server.sandboxPaths.BuildImportDestination(preset.SandboxName)
	if err != nil {
		return FolderAccessStatus{}, err
	}
	mirrorReady := false
	entryCount := 0
	if mirrorInfo, statErr := os.Stat(destinationAbsolutePath); statErr == nil && mirrorInfo.IsDir() {
		mirrorReady = granted
		entryCount = countDirectoryEntries(destinationAbsolutePath)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return FolderAccessStatus{}, fmt.Errorf("stat mirrored %s folder: %w", preset.ID, statErr)
	}

	return FolderAccessStatus{
		ID:                  preset.ID,
		Name:                preset.Name,
		Description:         preset.Description,
		Warning:             preset.Warning,
		Recommended:         preset.Recommended,
		AlwaysGranted:       preset.AlwaysGranted,
		Granted:             granted,
		HostPath:            hostPath,
		SandboxRelativePath: destinationRelativePath,
		SandboxAbsolutePath: sandbox.VirtualizeRelativeHomePath(destinationRelativePath),
		HostExists:          hostExists,
		MirrorReady:         mirrorReady,
		EntryCount:          entryCount,
	}, nil
}

func (server *Server) syncFolderAccessPreset(preset folderAccessPreset, granted bool, tokenClaims capabilityToken) (FolderAccessStatus, error) {
	// HostAccessOnly presets require no mirroring — just return the current status.
	if preset.HostAccessOnly {
		return server.folderAccessStatusForPreset(preset, granted)
	}

	hostPath, err := server.folderAccessPresetHostPath(preset)
	if err != nil {
		return FolderAccessStatus{}, err
	}
	if preset.CreateIfMissing {
		if err := os.MkdirAll(hostPath, 0o700); err != nil {
			return FolderAccessStatus{}, fmt.Errorf("create %s folder: %w", preset.ID, err)
		}
	}
	if err := server.sandboxPaths.Ensure(); err != nil {
		return FolderAccessStatus{}, fmt.Errorf("ensure sandbox paths: %w", err)
	}
	hostInfo, err := os.Stat(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return server.folderAccessStatusForPreset(preset, granted)
		}
		return FolderAccessStatus{}, fmt.Errorf("stat %s folder: %w", preset.ID, err)
	}
	if !hostInfo.IsDir() {
		return FolderAccessStatus{}, fmt.Errorf("%s folder must be a directory", preset.Name)
	}

	resolvedSourcePath, _, err := sandbox.ResolveHostSource(hostPath)
	if err != nil {
		return FolderAccessStatus{}, err
	}
	destinationAbsolutePath, destinationRelativePath, err := server.sandboxPaths.BuildImportDestination(preset.SandboxName)
	if err != nil {
		return FolderAccessStatus{}, err
	}

	entryCount := 0
	if _, err := sandbox.MirrorPathAtomicWithFinalize(resolvedSourcePath, destinationAbsolutePath, func(string) error {
		entryCount = countDirectoryEntries(destinationAbsolutePath)
		if err := server.logEvent("folder_access.synced", tokenClaims.ControlSessionID, map[string]interface{}{
			"control_session_id":    tokenClaims.ControlSessionID,
			"actor_label":           tokenClaims.ActorLabel,
			"client_session_label":  tokenClaims.ClientSessionLabel,
			"folder_access_id":      preset.ID,
			"folder_name":           preset.Name,
			"host_source_path":      resolvedSourcePath,
			"sandbox_relative_path": destinationRelativePath,
			"entry_count":           entryCount,
		}); err != nil {
			return fmt.Errorf("audit %s sync: %w", preset.ID, err)
		}
		return nil
	}); err != nil {
		return FolderAccessStatus{}, err
	}

	return FolderAccessStatus{
		ID:                  preset.ID,
		Name:                preset.Name,
		Description:         preset.Description,
		Warning:             preset.Warning,
		Recommended:         preset.Recommended,
		AlwaysGranted:       preset.AlwaysGranted,
		Granted:             granted,
		HostPath:            resolvedSourcePath,
		SandboxRelativePath: destinationRelativePath,
		SandboxAbsolutePath: sandbox.VirtualizeRelativeHomePath(destinationRelativePath),
		HostExists:          true,
		MirrorReady:         true,
		EntryCount:          entryCount,
	}, nil
}

func (server *Server) syncFolderAccessPresetIfNeeded(preset folderAccessPreset, tokenClaims capabilityToken, previousFingerprint string) (FolderAccessStatus, bool, string, error) {
	// HostAccessOnly presets never need a mirror sync — return status directly.
	if preset.HostAccessOnly {
		status, err := server.folderAccessStatusForPreset(preset, true)
		return status, false, "", err
	}

	hostPath, err := server.folderAccessPresetHostPath(preset)
	if err != nil {
		return FolderAccessStatus{}, false, "", err
	}
	if preset.CreateIfMissing {
		if err := os.MkdirAll(hostPath, 0o700); err != nil {
			return FolderAccessStatus{}, false, "", fmt.Errorf("create %s folder: %w", preset.ID, err)
		}
	}

	status, err := server.folderAccessStatusForPreset(preset, true)
	if err != nil {
		return FolderAccessStatus{}, false, "", err
	}
	if !status.HostExists {
		return status, false, "", nil
	}

	sourceFingerprint, err := folderAccessSourceFingerprint(status.HostPath)
	if err != nil {
		return FolderAccessStatus{}, false, "", err
	}
	if status.MirrorReady && previousFingerprint == sourceFingerprint {
		return status, false, sourceFingerprint, nil
	}

	syncedStatus, err := server.syncFolderAccessPreset(preset, true, tokenClaims)
	if err != nil {
		return FolderAccessStatus{}, false, "", err
	}
	return syncedStatus, true, sourceFingerprint, nil
}

func (server *Server) removeFolderAccessMirror(preset folderAccessPreset, tokenClaims capabilityToken) error {
	if preset.AlwaysGranted || preset.HostAccessOnly {
		return nil
	}
	if err := server.sandboxPaths.Ensure(); err != nil {
		return fmt.Errorf("ensure sandbox paths: %w", err)
	}
	destinationAbsolutePath, destinationRelativePath, err := server.sandboxPaths.BuildImportDestination(preset.SandboxName)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(destinationAbsolutePath); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil
		}
		return fmt.Errorf("stat mirrored %s folder: %w", preset.ID, statErr)
	}
	if err := os.RemoveAll(destinationAbsolutePath); err != nil {
		return fmt.Errorf("remove mirrored %s folder: %w", preset.ID, err)
	}
	if err := server.logEvent("folder_access.removed", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id":    tokenClaims.ControlSessionID,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
		"folder_access_id":      preset.ID,
		"folder_name":           preset.Name,
		"sandbox_relative_path": destinationRelativePath,
	}); err != nil {
		return fmt.Errorf("audit %s mirror removal: %w", preset.ID, err)
	}
	return nil
}

func (server *Server) folderAccessPresetHostPath(preset folderAccessPreset) (string, error) {
	if preset.ID == folderAccessSharedID {
		return server.defaultSharedFolderPath()
	}
	if server.resolveUserHomeDir == nil {
		return "", fmt.Errorf("resolve user home is unavailable")
	}
	userHomeDirectory, err := server.resolveUserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(userHomeDirectory, preset.HomeRelativeDir), nil
}

func folderAccessSourceFingerprint(sourcePath string) (string, error) {
	sourceHash := sha256.New()
	if _, err := io.WriteString(sourceHash, "folder-access:v1\n"); err != nil {
		return "", fmt.Errorf("seed folder access fingerprint: %w", err)
	}

	if err := filepath.WalkDir(sourcePath, func(entryPath string, directoryEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relativePath, err := filepath.Rel(sourcePath, entryPath)
		if err != nil {
			return fmt.Errorf("resolve relative path: %w", err)
		}
		virtualRelativePath := filepath.ToSlash(relativePath)
		if virtualRelativePath == "." {
			virtualRelativePath = ""
		}
		if directoryEntry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("folder access source contains symlink at %q", virtualRelativePath)
		}

		entryInfo, err := directoryEntry.Info()
		if err != nil {
			return fmt.Errorf("stat entry %q: %w", virtualRelativePath, err)
		}

		entryType := "file"
		switch {
		case entryInfo.IsDir():
			entryType = "dir"
		case entryInfo.Mode().IsRegular():
		default:
			return fmt.Errorf("folder access source entry %q must be a regular file or directory", virtualRelativePath)
		}

		if _, err := io.WriteString(sourceHash, entryType+"\x00"+virtualRelativePath+"\x00"); err != nil {
			return fmt.Errorf("hash entry identity: %w", err)
		}
		if _, err := io.WriteString(sourceHash, fmt.Sprintf("%d\x00%d\n", entryInfo.Size(), entryInfo.ModTime().UTC().UnixNano())); err != nil {
			return fmt.Errorf("hash entry metadata: %w", err)
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("fingerprint folder access source %q: %w", sourcePath, err)
	}

	return hex.EncodeToString(sourceHash.Sum(nil)), nil
}

func folderAccessPresetByID(presetID string) (folderAccessPreset, bool) {
	for _, preset := range defaultFolderAccessPresets() {
		if preset.ID == presetID {
			return preset, true
		}
	}
	return folderAccessPreset{}, false
}

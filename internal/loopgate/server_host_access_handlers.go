package loopgate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"morph/internal/sandbox"
	"morph/internal/secrets"
)

const (
	hostFolderReadMaxBytes = 2 * 1024 * 1024
	hostAccessPlanTTL      = 2 * time.Hour
	hostAccessMaxPlans     = 512
	// How long we remember successfully applied plan IDs so duplicate apply
	// attempts get an explicit "already applied" hint instead of "unknown".
	hostAccessAppliedPlanRetention = 48 * time.Hour
)

type hostAccessStoredPlan struct {
	ControlSessionID string
	FolderPresetID   string
	Operations       []hostOrganizePlanOp
	Summary          string
	CreatedAt        time.Time
}

type hostOrganizePlanOp struct {
	Kind string `json:"kind"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	Path string `json:"path,omitempty"`
}

// parseHostOrganizePlanJSON decodes plan_json from capability arguments. Models and gateways vary:
//   - JSON array text: [{"kind":"mkdir","path":"x"}]
//   - JSON string whose value is that array (double-encoded)
//   - Deeper string wrapping (bounded) for over-eager encoders
func parseHostOrganizePlanJSON(raw string) ([]hostOrganizePlanOp, error) {
	return parseHostOrganizePlanJSONDepth(strings.TrimSpace(raw), 3)
}

func parseHostOrganizePlanJSONDepth(raw string, unwrapBudget int) ([]hostOrganizePlanOp, error) {
	if raw == "" {
		return nil, fmt.Errorf("empty plan_json")
	}
	if unwrapBudget < 0 {
		return nil, fmt.Errorf("plan_json: too many nested JSON string wrappers")
	}
	var ops []hostOrganizePlanOp
	arrayErr := json.Unmarshal([]byte(raw), &ops)
	if arrayErr == nil {
		return ops, nil
	}
	var wrapped string
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil && strings.TrimSpace(wrapped) != "" {
		return parseHostOrganizePlanJSONDepth(strings.TrimSpace(wrapped), unwrapBudget-1)
	}
	return nil, fmt.Errorf("invalid plan_json: %w", arrayErr)
}

const maxPlanSummaryRunesForApprovalUI = 280

// hostPlanApplyApprovalOperatorFields returns human-readable fields for approval UIs when the
// pending capability is host.plan.apply. Omits raw paths and plan_json; safe for operator display.
func (server *Server) hostPlanApplyApprovalOperatorFields(planID string) map[string]interface{} {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return nil
	}
	server.hostAccessPlansMu.Lock()
	plan, found := server.hostAccessPlans[planID]
	server.hostAccessPlansMu.Unlock()
	if !found || plan == nil {
		return nil
	}
	presetByID := make(map[string]folderAccessPreset)
	for _, p := range defaultFolderAccessPresets() {
		presetByID[p.ID] = p
	}
	preset, presetOK := presetByID[plan.FolderPresetID]
	folderLabel := plan.FolderPresetID
	if presetOK {
		folderLabel = preset.Name
	}
	mkdirN, moveN := 0, 0
	for _, op := range plan.Operations {
		switch strings.ToLower(strings.TrimSpace(op.Kind)) {
		case "mkdir":
			mkdirN++
		case "move":
			moveN++
		}
	}
	out := map[string]interface{}{
		"host_folder_display_name": folderLabel,
		"plan_operation_count":     len(plan.Operations),
		"plan_mkdir_count":         mkdirN,
		"plan_move_count":          moveN,
	}
	summary := strings.TrimSpace(plan.Summary)
	if summary != "" {
		runes := []rune(summary)
		if len(runes) > maxPlanSummaryRunesForApprovalUI {
			summary = string(runes[:maxPlanSummaryRunesForApprovalUI]) + "…"
		}
		out["plan_summary"] = summary
	}
	var detailParts []string
	if mkdirN > 0 {
		detailParts = append(detailParts, fmt.Sprintf("create %d folder(s)", mkdirN))
	}
	if moveN > 0 {
		detailParts = append(detailParts, fmt.Sprintf("move %d file(s) or folders", moveN))
	}
	detailJoined := strings.Join(detailParts, ", ")
	if detailJoined == "" {
		detailJoined = fmt.Sprintf("run %d planned operation(s)", len(plan.Operations))
	}
	out["operator_intent_line"] = fmt.Sprintf(
		"Morph will %s in your granted “%s” folder on this Mac. Files are not moved until you approve.",
		detailJoined,
		folderLabel,
	)
	return out
}

func lookupFolderAccessPresetByKey(rawKey string) (folderAccessPreset, bool) {
	key := strings.TrimSpace(strings.ToLower(rawKey))
	if key == "" {
		return folderAccessPreset{}, false
	}
	presets := defaultFolderAccessPresets()
	for _, preset := range presets {
		if strings.ToLower(preset.ID) == key {
			return preset, true
		}
	}
	compactKey := strings.ReplaceAll(key, " ", "")
	for _, preset := range presets {
		compactName := strings.ReplaceAll(strings.ToLower(preset.Name), " ", "")
		if compactName == compactKey || strings.ToLower(preset.Name) == key {
			return preset, true
		}
	}
	return folderAccessPreset{}, false
}

func pathUnderResolvedHostRoot(resolvedRoot string, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimPrefix(rel, string(os.PathSeparator))
	if strings.Contains(rel, "..") {
		return "", fmt.Errorf("path must not contain parent segments")
	}
	rootClean := filepath.Clean(resolvedRoot)
	if rel == "" {
		return rootClean, nil
	}
	joined := filepath.Join(rootClean, filepath.FromSlash(rel))
	joinedClean := filepath.Clean(joined)
	relOut, err := filepath.Rel(rootClean, joinedClean)
	if err != nil || strings.HasPrefix(relOut, "..") {
		return "", fmt.Errorf("path escapes granted folder root")
	}
	return joinedClean, nil
}

func (server *Server) hostFolderPresetGranted(presetID string) (bool, error) {
	grantedSet, err := server.loadFolderAccessGrantedSet()
	if err != nil {
		return false, err
	}
	return grantedSet[presetID], nil
}

func (server *Server) resolveFolderHostPathForAccess(preset folderAccessPreset) (string, error) {
	hostPath, err := server.folderAccessPresetHostPath(preset)
	if err != nil {
		return "", err
	}
	resolvedSourcePath, _, err := sandbox.ResolveHostSource(hostPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolvedSourcePath)
	if err != nil {
		return "", fmt.Errorf("stat host folder: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("host folder path is not a directory")
	}
	return resolvedSourcePath, nil
}

// pruneExpiredHostAccessPlansLocked drops stale pending plans and applied-plan
// tombstones. Caller must hold hostAccessPlansMu.
func (server *Server) pruneExpiredHostAccessPlansLocked() {
	cutoff := server.now().UTC().Add(-hostAccessPlanTTL)
	for id, plan := range server.hostAccessPlans {
		if plan.CreatedAt.Before(cutoff) {
			delete(server.hostAccessPlans, id)
		}
	}
	for len(server.hostAccessPlans) > hostAccessMaxPlans {
		var oldestID string
		var oldest time.Time
		for id, plan := range server.hostAccessPlans {
			if oldestID == "" || plan.CreatedAt.Before(oldest) {
				oldestID = id
				oldest = plan.CreatedAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(server.hostAccessPlans, oldestID)
	}

	appliedCutoff := server.now().UTC().Add(-hostAccessAppliedPlanRetention)
	for id, appliedAt := range server.hostAccessAppliedPlanAt {
		if appliedAt.Before(appliedCutoff) {
			delete(server.hostAccessAppliedPlanAt, id)
		}
	}
}

func randomHostPlanID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func validateOrganizePlanOperations(ops []hostOrganizePlanOp) error {
	if len(ops) == 0 {
		return fmt.Errorf("plan must contain at least one operation")
	}
	if len(ops) > 200 {
		return fmt.Errorf("plan exceeds maximum operations")
	}
	for i, op := range ops {
		kind := strings.ToLower(strings.TrimSpace(op.Kind))
		switch kind {
		case "mkdir":
			if strings.TrimSpace(op.Path) == "" {
				return fmt.Errorf("op %d: mkdir requires path", i)
			}
			if strings.Contains(op.Path, "..") {
				return fmt.Errorf("op %d: path must not contain ..", i)
			}
		case "move":
			if strings.TrimSpace(op.From) == "" || strings.TrimSpace(op.To) == "" {
				return fmt.Errorf("op %d: move requires from and to", i)
			}
			if strings.Contains(op.From, "..") || strings.Contains(op.To, "..") {
				return fmt.Errorf("op %d: paths must not contain ..", i)
			}
		default:
			return fmt.Errorf("op %d: unknown kind %q (use mkdir or move)", i, op.Kind)
		}
	}
	return nil
}

func (server *Server) executeHostFolderListCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	preset, ok := lookupFolderAccessPresetByKey(capabilityRequest.Arguments["folder_name"])
	if !ok {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "unknown folder_name", DenialCodeInvalidCapabilityArguments)
	}
	granted, err := server.hostFolderPresetGranted(preset.ID)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}
	if !granted {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "folder is not granted for host access")
	}
	rootPath, err := server.resolveFolderHostPathForAccess(preset)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}
	listPath, err := pathUnderResolvedHostRoot(rootPath, capabilityRequest.Arguments["path"])
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeInvalidCapabilityArguments)
	}
	entries, err := os.ReadDir(listPath)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}

	type listEntry struct {
		Name    string `json:"name"`
		IsDir   bool   `json:"is_dir"`
		Size    int64  `json:"size,omitempty"`
		ModUnix int64  `json:"mod_time_unix,omitempty"`
	}
	out := make([]listEntry, 0, len(entries))
	for _, entry := range entries {
		le := listEntry{Name: entry.Name(), IsDir: entry.IsDir()}
		if info, infoErr := entry.Info(); infoErr == nil {
			if !entry.IsDir() {
				le.Size = info.Size()
			}
			le.ModUnix = info.ModTime().Unix()
		}
		out = append(out, le)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })

	// Cap the number of entries sent to the model. A directory with hundreds of files
	// would produce a tool result large enough that the JSON gets truncated mid-entry,
	// which can confuse cloud model APIs. The model gets the first N entries plus a
	// count of how many were omitted; that is enough to draft an organize plan.
	const maxFolderListEntries = 100
	totalCount := len(out)
	omittedCount := 0
	if totalCount > maxFolderListEntries {
		omittedCount = totalCount - maxFolderListEntries
		out = out[:maxFolderListEntries]
	}

	relDisplay := ""
	if relOut, relErr := filepath.Rel(rootPath, listPath); relErr == nil && relOut != "." {
		relDisplay = filepath.ToSlash(relOut)
	}

	structured := map[string]interface{}{
		"folder_id":    preset.ID,
		"folder_name":  preset.Name,
		"path":         relDisplay,
		"total_count":  totalCount,
		"entries":      toJSONSlice(out),
	}
	if omittedCount > 0 {
		structured["omitted_count"] = omittedCount
		structured["omitted_note"] = fmt.Sprintf("%d additional entries were omitted; the listing above shows the first %d files sorted alphabetically.", omittedCount, maxFolderListEntries)
	}
	return server.hostAccessStructuredSuccess(tokenClaims, capabilityRequest, structured, hostAccessListClassification())
}

func (server *Server) executeHostFolderReadCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	preset, ok := lookupFolderAccessPresetByKey(capabilityRequest.Arguments["folder_name"])
	if !ok {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "unknown folder_name", DenialCodeInvalidCapabilityArguments)
	}
	granted, err := server.hostFolderPresetGranted(preset.ID)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}
	if !granted {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "folder is not granted for host access")
	}
	rootPath, err := server.resolveFolderHostPathForAccess(preset)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}
	filePath, err := pathUnderResolvedHostRoot(rootPath, capabilityRequest.Arguments["path"])
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeInvalidCapabilityArguments)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}
	if info.IsDir() {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "path is a directory, not a file", DenialCodeInvalidCapabilityArguments)
	}
	fileHandle, err := os.Open(filePath)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}
	defer fileHandle.Close()

	limitedReader := io.LimitReader(fileHandle, hostFolderReadMaxBytes+1)
	raw, err := io.ReadAll(limitedReader)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}
	if int64(len(raw)) > hostFolderReadMaxBytes {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "file exceeds host read size limit", DenialCodeFsReadSizeLimitExceeded)
	}

	relShow := ""
	if relOut, relErr := filepath.Rel(rootPath, filePath); relErr == nil {
		relShow = filepath.ToSlash(relOut)
	}
	structured := map[string]interface{}{
		"folder_id":   preset.ID,
		"folder_name": preset.Name,
		"path":        relShow,
		"byte_length": len(raw),
	}
	if utf8.Valid(raw) {
		structured["content"] = string(raw)
		structured["encoding"] = "utf-8"
	} else {
		structured["encoding"] = "binary"
		structured["content_elided"] = true
	}

	return server.hostAccessStructuredSuccess(tokenClaims, capabilityRequest, structured, hostAccessReadClassification())
}

func (server *Server) executeHostOrganizePlanCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	preset, ok := lookupFolderAccessPresetByKey(capabilityRequest.Arguments["folder_name"])
	if !ok {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "unknown folder_name", DenialCodeInvalidCapabilityArguments)
	}
	granted, err := server.hostFolderPresetGranted(preset.ID)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}
	if !granted {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "folder is not granted for host access")
	}
	if _, err := server.resolveFolderHostPathForAccess(preset); err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}

	ops, err := parseHostOrganizePlanJSON(capabilityRequest.Arguments["plan_json"])
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeInvalidCapabilityArguments)
	}
	if err := validateOrganizePlanOperations(ops); err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeInvalidCapabilityArguments)
	}

	planID, err := randomHostPlanID()
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "failed to mint plan id", DenialCodeExecutionFailed)
	}

	server.hostAccessPlansMu.Lock()
	server.pruneExpiredHostAccessPlansLocked()
	server.hostAccessPlans[planID] = &hostAccessStoredPlan{
		ControlSessionID: tokenClaims.ControlSessionID,
		FolderPresetID:   preset.ID,
		Operations:       ops,
		Summary:          capabilityRequest.Arguments["summary"],
		CreatedAt:        server.now().UTC(),
	}
	server.hostAccessPlansMu.Unlock()

	structured := map[string]interface{}{
		"plan_id":     planID,
		"folder_id":   preset.ID,
		"folder_name": preset.Name,
		"summary":     strings.TrimSpace(capabilityRequest.Arguments["summary"]),
		"op_count":    len(ops),
	}
	return server.hostAccessStructuredSuccess(tokenClaims, capabilityRequest, structured, hostAccessPlanClassification())
}

func (server *Server) executeHostPlanApplyCapability(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) CapabilityResponse {
	planID := strings.TrimSpace(capabilityRequest.Arguments["plan_id"])
	if planID == "" {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "missing plan_id", DenialCodeInvalidCapabilityArguments)
	}

	server.hostAccessPlansMu.Lock()
	server.pruneExpiredHostAccessPlansLocked()
	plan, found := server.hostAccessPlans[planID]
	_, alreadyApplied := server.hostAccessAppliedPlanAt[planID]
	server.hostAccessPlansMu.Unlock()
	if !found {
		if alreadyApplied {
			return hostAccessErrorResponse(server, tokenClaims, capabilityRequest,
				"plan_id was already used: host.plan.apply succeeded earlier and each plan_id is single-use. If you need more host folder changes, call host.organize.plan again to mint a new plan_id, then approve host.plan.apply for that new id.",
				DenialCodeInvalidCapabilityArguments)
		}
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest,
			"no stored plan matches this plan_id (wrong id, plan expired, Loopgate restarted, or it was evicted). Call host.organize.plan again with the same folder_name and an updated plan_json to mint a fresh plan_id.",
			DenialCodeInvalidCapabilityArguments)
	}
	if plan.ControlSessionID != tokenClaims.ControlSessionID {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "plan belongs to a different control session")
	}
	if server.now().UTC().Sub(plan.CreatedAt) > hostAccessPlanTTL {
		server.hostAccessPlansMu.Lock()
		delete(server.hostAccessPlans, planID)
		server.hostAccessPlansMu.Unlock()
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "plan has expired", DenialCodeInvalidCapabilityArguments)
	}

	presetByID := make(map[string]folderAccessPreset)
	for _, p := range defaultFolderAccessPresets() {
		presetByID[p.ID] = p
	}
	preset, ok := presetByID[plan.FolderPresetID]
	if !ok {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "stored plan references unknown folder", DenialCodeExecutionFailed)
	}
	granted, err := server.hostFolderPresetGranted(preset.ID)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}
	if !granted {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "folder is no longer granted")
	}
	rootPath, err := server.resolveFolderHostPathForAccess(preset)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), DenialCodeExecutionFailed)
	}

	type applyResult struct {
		Step   int    `json:"step"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}
	results := make([]applyResult, 0, len(plan.Operations))

	mkdirOps := make([]hostOrganizePlanOp, 0)
	moveOps := make([]hostOrganizePlanOp, 0)
	for _, op := range plan.Operations {
		switch strings.ToLower(strings.TrimSpace(op.Kind)) {
		case "mkdir":
			mkdirOps = append(mkdirOps, op)
		case "move":
			moveOps = append(moveOps, op)
		}
	}
	sort.Slice(mkdirOps, func(i, j int) bool {
		return depthScore(mkdirOps[i].Path) < depthScore(mkdirOps[j].Path)
	})

	step := 0
	for _, op := range mkdirOps {
		step++
		target, pathErr := pathUnderResolvedHostRoot(rootPath, op.Path)
		if pathErr != nil {
			results = append(results, applyResult{Step: step, Kind: "mkdir", Status: "error", Detail: pathErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		if mkdirErr := os.MkdirAll(target, 0o755); mkdirErr != nil {
			results = append(results, applyResult{Step: step, Kind: "mkdir", Status: "error", Detail: mkdirErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		results = append(results, applyResult{Step: step, Kind: "mkdir", Status: "ok", Detail: filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(target, rootPath), string(os.PathSeparator)))})
	}

	for _, op := range moveOps {
		step++
		fromPath, fromErr := pathUnderResolvedHostRoot(rootPath, op.From)
		if fromErr != nil {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: fromErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		toPath, toErr := pathUnderResolvedHostRoot(rootPath, op.To)
		if toErr != nil {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: toErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		if _, statErr := os.Stat(fromPath); statErr != nil {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: statErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(toPath), 0o755); mkdirErr != nil {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: mkdirErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		if renameErr := os.Rename(fromPath, toPath); renameErr != nil {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: renameErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		results = append(results, applyResult{Step: step, Kind: "move", Status: "ok"})
	}

	server.hostAccessPlansMu.Lock()
	if server.hostAccessAppliedPlanAt == nil {
		server.hostAccessAppliedPlanAt = make(map[string]time.Time)
	}
	server.hostAccessAppliedPlanAt[planID] = server.now().UTC()
	server.pruneExpiredHostAccessPlansLocked()
	delete(server.hostAccessPlans, planID)
	server.hostAccessPlansMu.Unlock()

	structured := map[string]interface{}{
		"plan_id": planID,
		"results": toJSONSlice(results),
	}
	return server.hostAccessStructuredSuccess(tokenClaims, capabilityRequest, structured, hostAccessApplyClassification())
}

// toJSONSlice converts a typed Go slice to []interface{} by JSON round-trip.
// This is required because fieldsMetadataForStructuredResult type-switches on
// []interface{} to detect arrays — typed slices fall into the default case and
// get misclassified as scalars, causing Validate() to reject the result.
func toJSONSlice(v interface{}) []interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out []interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func depthScore(p string) int {
	p = strings.Trim(p, `/\`)
	if p == "" {
		return 0
	}
	return strings.Count(filepath.ToSlash(p), "/") + 1
}

func hostAccessListClassification() ResultClassification {
	return ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
			Memory: true,
		},
	}
}

func hostAccessReadClassification() ResultClassification {
	return ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
			Memory: false,
		},
	}
}

func hostAccessPlanClassification() ResultClassification {
	return ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
			Memory: true,
		},
	}
}

func hostAccessApplyClassification() ResultClassification {
	return ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
			Memory: false,
		},
	}
}

func (server *Server) hostAccessStructuredSuccess(tokenClaims capabilityToken, capabilityRequest CapabilityRequest, structured map[string]interface{}, classification ResultClassification) CapabilityResponse {
	fieldsMeta, err := fieldsMetadataForStructuredResult(structured, ResultFieldOriginLocal, classification)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "internal result metadata error", DenialCodeExecutionFailed)
	}
	resultMetadata := map[string]interface{}{
		"prompt_eligible": classification.PromptEligible(),
		"memory_eligible": classification.MemoryEligible(),
		"display_only":     classification.DisplayOnly(),
		"audit_only":       classification.AuditOnly(),
		"quarantined":      classification.Quarantined(),
	}
	if err := server.logEvent("capability.executed", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":            capabilityRequest.RequestID,
		"capability":            capabilityRequest.Capability,
		"status":                ResponseStatusSuccess,
		"result_classification": classification,
		"result_provenance":     resultMetadata,
		"actor_label":           tokenClaims.ActorLabel,
		"client_session_label":  tokenClaims.ClientSessionLabel,
		"control_session_id":    tokenClaims.ControlSessionID,
		"token_id":              tokenClaims.TokenID,
		"parent_token_id":       tokenClaims.ParentTokenID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	successResponse := CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           ResponseStatusSuccess,
		StructuredResult: structured,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		Metadata:         resultMetadata,
	}
	server.emitUIToolResult(tokenClaims.ControlSessionID, capabilityRequest, successResponse)
	return successResponse
}

func hostAccessApplyPartialFailure(server *Server, tokenClaims capabilityToken, capabilityRequest CapabilityRequest, results interface{}) CapabilityResponse {
	structured := map[string]interface{}{
		"partial": true,
		"results": toJSONSlice(results),
	}
	classification := hostAccessApplyClassification()
	fieldsMeta, err := fieldsMetadataForStructuredResult(structured, ResultFieldOriginLocal, classification)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "internal result metadata error", DenialCodeExecutionFailed)
	}
	if auditErr := server.logEvent("capability.error", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":             capabilityRequest.RequestID,
		"capability":             capabilityRequest.Capability,
		"error":                  "host plan apply stopped after partial execution",
		"operator_error_class":   "host_access_partial_execution",
		"actor_label":            tokenClaims.ActorLabel,
		"client_session_label":   tokenClaims.ClientSessionLabel,
		"control_session_id":     tokenClaims.ControlSessionID,
	}); auditErr != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	resp := CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           ResponseStatusError,
		DenialReason:     "host plan apply failed partway through; some operations may have succeeded",
		DenialCode:       DenialCodeExecutionFailed,
		StructuredResult: structured,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		Redacted:         true,
	}
	server.emitUIToolResult(tokenClaims.ControlSessionID, capabilityRequest, resp)
	return resp
}

func hostAccessErrorResponse(server *Server, tokenClaims capabilityToken, capabilityRequest CapabilityRequest, message string, code string) CapabilityResponse {
	if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"reason":               secrets.RedactText(message),
		"denial_code":          code,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	resp := CapabilityResponse{
		RequestID:    capabilityRequest.RequestID,
		Status:       ResponseStatusError,
		DenialReason: message,
		DenialCode:   code,
		Redacted:     true,
	}
	server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, code, message)
	return resp
}

func hostAccessDeniedResponse(server *Server, tokenClaims capabilityToken, capabilityRequest CapabilityRequest, message string) CapabilityResponse {
	if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"reason":               secrets.RedactText(message),
		"denial_code":          DenialCodePolicyDenied,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	resp := CapabilityResponse{
		RequestID:    capabilityRequest.RequestID,
		Status:       ResponseStatusDenied,
		DenialReason: message,
		DenialCode:   DenialCodePolicyDenied,
	}
	server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, DenialCodePolicyDenied, message)
	return resp
}

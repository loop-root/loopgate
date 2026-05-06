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

	"golang.org/x/sys/unix"
	"loopgate/internal/hostaccess"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	policypkg "loopgate/internal/policy"
	"loopgate/internal/sandbox"
	"loopgate/internal/secrets"
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

func hostAccessPathErrorCode(err error) string {
	if hostaccess.IsPathPolicyError(err) {
		return controlapipkg.DenialCodeInvalidCapabilityArguments
	}
	return controlapipkg.DenialCodeExecutionFailed
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
	server.hostAccessRuntime.mu.Lock()
	plan, found := server.hostAccessRuntime.plans[planID]
	server.hostAccessRuntime.mu.Unlock()
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
		"Loopgate-governed actions will %s in your granted “%s” folder on this Mac. Files are not moved until you approve.",
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
// tombstones. Caller must hold hostAccessRuntime.mu.
func (server *Server) pruneExpiredHostAccessPlansLocked() {
	cutoff := server.now().UTC().Add(-hostAccessPlanTTL)
	for id, plan := range server.hostAccessRuntime.plans {
		if plan.CreatedAt.Before(cutoff) {
			delete(server.hostAccessRuntime.plans, id)
		}
	}
	for len(server.hostAccessRuntime.plans) > hostAccessMaxPlans {
		var oldestID string
		var oldest time.Time
		for id, plan := range server.hostAccessRuntime.plans {
			if oldestID == "" || plan.CreatedAt.Before(oldest) {
				oldestID = id
				oldest = plan.CreatedAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(server.hostAccessRuntime.plans, oldestID)
	}

	appliedCutoff := server.now().UTC().Add(-hostAccessAppliedPlanRetention)
	for id, appliedAt := range server.hostAccessRuntime.appliedPlanAt {
		if appliedAt.Before(appliedCutoff) {
			delete(server.hostAccessRuntime.appliedPlanAt, id)
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
				return fmt.Errorf("op %d: path must not contain dot-dot segments", i)
			}
		case "move":
			if strings.TrimSpace(op.From) == "" || strings.TrimSpace(op.To) == "" {
				return fmt.Errorf("op %d: move requires from and to", i)
			}
			if strings.Contains(op.From, "..") || strings.Contains(op.To, "..") {
				return fmt.Errorf("op %d: paths must not contain dot-dot segments", i)
			}
		default:
			return fmt.Errorf("op %d: unknown kind %q (use mkdir or move)", i, op.Kind)
		}
	}
	return nil
}

func (server *Server) executeHostFolderListCapability(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) controlapipkg.CapabilityResponse {
	preset, ok := lookupFolderAccessPresetByKey(capabilityRequest.Arguments["folder_name"])
	if !ok {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "unknown folder_name", controlapipkg.DenialCodeInvalidCapabilityArguments)
	}
	granted, err := server.hostFolderPresetGranted(preset.ID)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
	}
	if !granted {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "folder is not granted for host access")
	}
	rootPath, err := server.resolveFolderHostPathForAccess(preset)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
	}
	directoryHandle, listPath, err := hostaccess.OpenPathReadOnly(rootPath, capabilityRequest.Arguments["path"], true)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), hostAccessPathErrorCode(err))
	}
	defer directoryHandle.Close()

	entries, err := directoryHandle.ReadDir(-1)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
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

	structured := map[string]interface{}{
		"folder_id":   preset.ID,
		"folder_name": preset.Name,
		"path":        listPath.Display,
		"total_count": totalCount,
		"entries":     toJSONSlice(out),
	}
	if omittedCount > 0 {
		structured["omitted_count"] = omittedCount
		structured["omitted_note"] = fmt.Sprintf("%d additional entries were omitted; the listing above shows the first %d files sorted alphabetically.", omittedCount, maxFolderListEntries)
	}
	return server.hostAccessStructuredSuccess(tokenClaims, capabilityRequest, structured, hostAccessListClassification())
}

func (server *Server) executeHostFolderReadCapability(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) controlapipkg.CapabilityResponse {
	preset, ok := lookupFolderAccessPresetByKey(capabilityRequest.Arguments["folder_name"])
	if !ok {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "unknown folder_name", controlapipkg.DenialCodeInvalidCapabilityArguments)
	}
	granted, err := server.hostFolderPresetGranted(preset.ID)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
	}
	if !granted {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "folder is not granted for host access")
	}
	rootPath, err := server.resolveFolderHostPathForAccess(preset)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
	}
	fileHandle, filePath, err := hostaccess.OpenPathReadOnly(rootPath, capabilityRequest.Arguments["path"], false)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), hostAccessPathErrorCode(err))
	}
	defer fileHandle.Close()

	limitedReader := io.LimitReader(fileHandle, hostFolderReadMaxBytes+1)
	raw, err := io.ReadAll(limitedReader)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
	}
	if int64(len(raw)) > hostFolderReadMaxBytes {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "file exceeds host read size limit", controlapipkg.DenialCodeFsReadSizeLimitExceeded)
	}

	structured := map[string]interface{}{
		"folder_id":   preset.ID,
		"folder_name": preset.Name,
		"path":        filePath.Display,
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

func (server *Server) executeHostOrganizePlanCapability(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) controlapipkg.CapabilityResponse {
	preset, ok := lookupFolderAccessPresetByKey(capabilityRequest.Arguments["folder_name"])
	if !ok {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "unknown folder_name", controlapipkg.DenialCodeInvalidCapabilityArguments)
	}
	granted, err := server.hostFolderPresetGranted(preset.ID)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
	}
	if !granted {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "folder is not granted for host access")
	}
	if _, err := server.resolveFolderHostPathForAccess(preset); err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
	}

	ops, err := parseHostOrganizePlanJSON(capabilityRequest.Arguments["plan_json"])
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeInvalidCapabilityArguments)
	}
	if err := validateOrganizePlanOperations(ops); err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeInvalidCapabilityArguments)
	}

	planID, err := randomHostPlanID()
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "failed to mint plan id", controlapipkg.DenialCodeExecutionFailed)
	}

	server.hostAccessRuntime.mu.Lock()
	server.pruneExpiredHostAccessPlansLocked()
	server.hostAccessRuntime.plans[planID] = &hostAccessStoredPlan{
		ControlSessionID: tokenClaims.ControlSessionID,
		FolderPresetID:   preset.ID,
		Operations:       ops,
		Summary:          capabilityRequest.Arguments["summary"],
		CreatedAt:        server.now().UTC(),
	}
	server.hostAccessRuntime.mu.Unlock()

	structured := map[string]interface{}{
		"plan_id":     planID,
		"folder_id":   preset.ID,
		"folder_name": preset.Name,
		"summary":     strings.TrimSpace(capabilityRequest.Arguments["summary"]),
		"op_count":    len(ops),
	}
	return server.hostAccessStructuredSuccess(tokenClaims, capabilityRequest, structured, hostAccessPlanClassification())
}

func (server *Server) executeHostPlanApplyCapability(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) controlapipkg.CapabilityResponse {
	planID := strings.TrimSpace(capabilityRequest.Arguments["plan_id"])
	if planID == "" {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "missing plan_id", controlapipkg.DenialCodeInvalidCapabilityArguments)
	}

	server.hostAccessRuntime.mu.Lock()
	server.pruneExpiredHostAccessPlansLocked()
	plan, found := server.hostAccessRuntime.plans[planID]
	_, alreadyApplied := server.hostAccessRuntime.appliedPlanAt[planID]
	server.hostAccessRuntime.mu.Unlock()
	if !found {
		if alreadyApplied {
			return hostAccessErrorResponse(server, tokenClaims, capabilityRequest,
				"plan_id was already used: host.plan.apply succeeded earlier and each plan_id is single-use. If you need more host folder changes, call host.organize.plan again to mint a new plan_id, then approve host.plan.apply for that new id.",
				controlapipkg.DenialCodeInvalidCapabilityArguments)
		}
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest,
			"no stored plan matches this plan_id (wrong id, plan expired, Loopgate restarted, or it was evicted). Call host.organize.plan again with the same folder_name and an updated plan_json to mint a fresh plan_id.",
			controlapipkg.DenialCodeInvalidCapabilityArguments)
	}
	if plan.ControlSessionID != tokenClaims.ControlSessionID {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "plan belongs to a different control session")
	}
	if server.now().UTC().Sub(plan.CreatedAt) > hostAccessPlanTTL {
		server.hostAccessRuntime.mu.Lock()
		delete(server.hostAccessRuntime.plans, planID)
		server.hostAccessRuntime.mu.Unlock()
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "plan has expired", controlapipkg.DenialCodeInvalidCapabilityArguments)
	}

	presetByID := make(map[string]folderAccessPreset)
	for _, p := range defaultFolderAccessPresets() {
		presetByID[p.ID] = p
	}
	preset, ok := presetByID[plan.FolderPresetID]
	if !ok {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "stored plan references unknown folder", controlapipkg.DenialCodeExecutionFailed)
	}
	granted, err := server.hostFolderPresetGranted(preset.ID)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
	}
	if !granted {
		return hostAccessDeniedResponse(server, tokenClaims, capabilityRequest, "folder is no longer granted")
	}
	rootPath, err := server.resolveFolderHostPathForAccess(preset)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, err.Error(), controlapipkg.DenialCodeExecutionFailed)
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
		target, pathErr := hostaccess.EnsureDirectoryUnderRoot(rootPath, op.Path, 0o755)
		if pathErr != nil {
			results = append(results, applyResult{Step: step, Kind: "mkdir", Status: "error", Detail: pathErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		results = append(results, applyResult{Step: step, Kind: "mkdir", Status: "ok", Detail: target.Display})
	}

	for _, op := range moveOps {
		step++
		_, fromStat, fromExists, fromErr := hostaccess.LstatPathUnderRoot(rootPath, op.From)
		if fromErr != nil {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: fromErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		if !fromExists {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: "source path does not exist"})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		if hostaccess.PathModeIsSymlink(uint32(fromStat.Mode)) {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: "source path traverses a symlink, which is not allowed"})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		toParentPath := filepath.Dir(op.To)
		if toParentPath == "." {
			toParentPath = ""
		}
		if _, mkdirErr := hostaccess.EnsureDirectoryUnderRoot(rootPath, toParentPath, 0o755); mkdirErr != nil {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: mkdirErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}

		fromParentHandle, fromBaseName, _, fromParentErr := hostaccess.OpenParentDirectory(rootPath, op.From)
		if fromParentErr != nil {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: fromParentErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		toParentHandle, toBaseName, toPath, toErr := hostaccess.OpenParentDirectory(rootPath, op.To)
		if toErr != nil {
			fromParentHandle.Close()
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: toErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		renameErr := unix.Renameat(int(fromParentHandle.Fd()), fromBaseName, int(toParentHandle.Fd()), toBaseName)
		fromParentHandle.Close()
		toParentHandle.Close()
		if renameErr != nil {
			results = append(results, applyResult{Step: step, Kind: "move", Status: "error", Detail: renameErr.Error()})
			return hostAccessApplyPartialFailure(server, tokenClaims, capabilityRequest, results)
		}
		results = append(results, applyResult{Step: step, Kind: "move", Status: "ok", Detail: toPath.Display})
	}

	server.hostAccessRuntime.mu.Lock()
	if server.hostAccessRuntime.appliedPlanAt == nil {
		server.hostAccessRuntime.appliedPlanAt = make(map[string]time.Time)
	}
	server.hostAccessRuntime.appliedPlanAt[planID] = server.now().UTC()
	server.pruneExpiredHostAccessPlansLocked()
	delete(server.hostAccessRuntime.plans, planID)
	server.hostAccessRuntime.mu.Unlock()

	structured := map[string]interface{}{
		"plan_id": planID,
		"results": toJSONSlice(results),
	}
	return server.hostAccessStructuredSuccess(tokenClaims, capabilityRequest, structured, hostAccessApplyClassification())
}

func (server *Server) autoAllowLowRiskHostPlanApply(controlSessionID string, capabilityRequest controlapipkg.CapabilityRequest, current policypkg.CheckResult) (policypkg.CheckResult, bool) {
	if current.Decision != policypkg.NeedsApproval || strings.TrimSpace(capabilityRequest.Capability) != "host.plan.apply" {
		return current, false
	}
	if !server.isLowRiskHostPlanApply(controlSessionID, capabilityRequest.Arguments["plan_id"]) {
		return current, false
	}
	return policypkg.CheckResult{
		Decision: policypkg.Allow,
		Reason:   "bounded move-only host organization plan stays within a granted folder",
	}, true
}

func (server *Server) isLowRiskHostPlanApply(controlSessionID string, planID string) bool {
	planID = strings.TrimSpace(planID)
	controlSessionID = strings.TrimSpace(controlSessionID)
	if planID == "" || controlSessionID == "" {
		return false
	}

	server.hostAccessRuntime.mu.Lock()
	server.pruneExpiredHostAccessPlansLocked()
	plan, found := server.hostAccessRuntime.plans[planID]
	server.hostAccessRuntime.mu.Unlock()
	if !found || plan == nil {
		return false
	}
	if plan.ControlSessionID != controlSessionID {
		return false
	}
	if server.now().UTC().Sub(plan.CreatedAt) > hostAccessPlanTTL {
		return false
	}

	presetByID := make(map[string]folderAccessPreset)
	for _, p := range defaultFolderAccessPresets() {
		presetByID[p.ID] = p
	}
	preset, ok := presetByID[plan.FolderPresetID]
	if !ok {
		return false
	}
	granted, err := server.hostFolderPresetGranted(preset.ID)
	if err != nil || !granted {
		return false
	}
	rootPath, err := server.resolveFolderHostPathForAccess(preset)
	if err != nil {
		return false
	}

	mkdirOps := make([]hostOrganizePlanOp, 0)
	moveOps := make([]hostOrganizePlanOp, 0)
	for _, op := range plan.Operations {
		switch strings.ToLower(strings.TrimSpace(op.Kind)) {
		case "mkdir":
			mkdirOps = append(mkdirOps, op)
		case "move":
			moveOps = append(moveOps, op)
		default:
			return false
		}
	}
	sort.Slice(mkdirOps, func(i, j int) bool {
		return depthScore(mkdirOps[i].Path) < depthScore(mkdirOps[j].Path)
	})

	plannedDirectories := make(map[string]struct{})
	for _, op := range mkdirOps {
		normalizedPath, ok := hostPlanPathIsLowRisk(rootPath, op.Path, plannedDirectories)
		if !ok {
			return false
		}
		plannedDirectories[normalizedPath.Display] = struct{}{}
	}
	for _, op := range moveOps {
		if !hostPlanMoveIsLowRisk(rootPath, op.From, op.To, plannedDirectories) {
			return false
		}
	}
	return len(plan.Operations) > 0
}

func hostPlanMoveIsLowRisk(rootPath string, from string, to string, plannedDirectories map[string]struct{}) bool {
	fromPath, fromStat, fromExists, err := hostaccess.LstatPathUnderRoot(rootPath, from)
	if err != nil || hostPlanContainsHiddenSegment(from) {
		return false
	}
	toPath, err := hostaccess.NormalizeRelativePath(to)
	if err != nil || hostPlanContainsHiddenSegment(to) || len(toPath.Parts) == 0 {
		return false
	}
	if fromPath.Display == toPath.Display {
		return false
	}
	var toStat unix.Stat_t
	toExists := false
	if !hostPlanParentIsPlanned(toPath, plannedDirectories) {
		var statErr error
		_, toStat, toExists, statErr = hostaccess.LstatPathUnderRoot(rootPath, toPath.Display)
		if statErr != nil {
			return false
		}
	}
	if toExists {
		return false
	}
	if hostaccess.PathModeIsSymlink(uint32(toStat.Mode)) {
		return false
	}
	if !fromExists {
		return true
	}
	if hostaccess.PathModeIsSymlink(uint32(fromStat.Mode)) || hostaccess.PathModeIsDirectory(uint32(fromStat.Mode)) {
		return false
	}
	return true
}

func hostPlanPathIsLowRisk(rootPath string, rel string, plannedDirectories map[string]struct{}) (hostaccess.RelativePath, bool) {
	if hostPlanContainsHiddenSegment(rel) {
		return hostaccess.RelativePath{}, false
	}
	normalizedPath, statResult, exists, err := hostaccess.LstatPathUnderRoot(rootPath, rel)
	if err != nil || len(normalizedPath.Parts) == 0 {
		return hostaccess.RelativePath{}, false
	}
	if exists {
		return normalizedPath, hostaccess.PathModeIsDirectory(uint32(statResult.Mode))
	}
	if hostPlanParentIsPlanned(normalizedPath, plannedDirectories) {
		return normalizedPath, true
	}
	parentHandle, _, _, err := hostaccess.OpenParentDirectory(rootPath, rel)
	if err != nil {
		return hostaccess.RelativePath{}, false
	}
	parentHandle.Close()
	return normalizedPath, true
}

func hostPlanParentIsPlanned(path hostaccess.RelativePath, plannedDirectories map[string]struct{}) bool {
	if len(path.Parts) <= 1 {
		return false
	}
	_, planned := plannedDirectories[strings.Join(path.Parts[:len(path.Parts)-1], "/")]
	return planned
}

func hostPlanContainsHiddenSegment(rel string) bool {
	trimmed := strings.Trim(strings.TrimSpace(rel), `/\`)
	if trimmed == "" {
		return false
	}
	for _, segment := range strings.Split(filepath.ToSlash(trimmed), "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
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

func hostAccessListClassification() controlapipkg.ResultClassification {
	return controlapipkg.ResultClassification{
		Exposure: controlapipkg.ResultExposureDisplay,
		Eligibility: controlapipkg.ResultEligibility{
			Prompt: true,
		},
	}
}

func hostAccessReadClassification() controlapipkg.ResultClassification {
	return controlapipkg.ResultClassification{
		Exposure: controlapipkg.ResultExposureDisplay,
		Eligibility: controlapipkg.ResultEligibility{
			Prompt: true,
		},
	}
}

func hostAccessPlanClassification() controlapipkg.ResultClassification {
	return controlapipkg.ResultClassification{
		Exposure: controlapipkg.ResultExposureDisplay,
		Eligibility: controlapipkg.ResultEligibility{
			Prompt: true,
		},
	}
}

func hostAccessApplyClassification() controlapipkg.ResultClassification {
	return controlapipkg.ResultClassification{
		Exposure: controlapipkg.ResultExposureDisplay,
		Eligibility: controlapipkg.ResultEligibility{
			Prompt: true,
		},
	}
}

func (server *Server) hostAccessStructuredSuccess(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, structured map[string]interface{}, classification controlapipkg.ResultClassification) controlapipkg.CapabilityResponse {
	fieldsMeta, err := fieldsMetadataForStructuredResult(structured, controlapipkg.ResultFieldOriginLocal, classification)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "internal result metadata error", controlapipkg.DenialCodeExecutionFailed)
	}
	resultMetadata := map[string]interface{}{
		"prompt_eligible": classification.PromptEligible(),
		"display_only":    classification.DisplayOnly(),
		"audit_only":      classification.AuditOnly(),
		"quarantined":     classification.Quarantined(),
	}
	if err := server.logEvent("capability.executed", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":            capabilityRequest.RequestID,
		"capability":            capabilityRequest.Capability,
		"status":                controlapipkg.ResponseStatusSuccess,
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
	successResponse := controlapipkg.CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           controlapipkg.ResponseStatusSuccess,
		StructuredResult: structured,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		Metadata:         resultMetadata,
	}
	server.emitUIToolResult(tokenClaims.ControlSessionID, capabilityRequest, successResponse)
	return successResponse
}

func hostAccessApplyPartialFailure(server *Server, tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, results interface{}) controlapipkg.CapabilityResponse {
	structured := map[string]interface{}{
		"partial": true,
		"results": toJSONSlice(results),
	}
	classification := hostAccessApplyClassification()
	fieldsMeta, err := fieldsMetadataForStructuredResult(structured, controlapipkg.ResultFieldOriginLocal, classification)
	if err != nil {
		return hostAccessErrorResponse(server, tokenClaims, capabilityRequest, "internal result metadata error", controlapipkg.DenialCodeExecutionFailed)
	}
	if auditErr := server.logEvent("capability.error", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"error":                "host plan apply stopped after partial execution",
		"operator_error_class": "host_access_partial_execution",
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}); auditErr != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	resp := controlapipkg.CapabilityResponse{
		RequestID:        capabilityRequest.RequestID,
		Status:           controlapipkg.ResponseStatusError,
		DenialReason:     "host plan apply failed partway through; some operations may have succeeded",
		DenialCode:       controlapipkg.DenialCodeExecutionFailed,
		StructuredResult: structured,
		FieldsMeta:       fieldsMeta,
		Classification:   classification,
		Redacted:         true,
	}
	server.emitUIToolResult(tokenClaims.ControlSessionID, capabilityRequest, resp)
	return resp
}

func hostAccessErrorResponse(server *Server, tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, message string, code string) controlapipkg.CapabilityResponse {
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
	resp := controlapipkg.CapabilityResponse{
		RequestID:    capabilityRequest.RequestID,
		Status:       controlapipkg.ResponseStatusError,
		DenialReason: message,
		DenialCode:   code,
		Redacted:     true,
	}
	server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, code, message)
	return resp
}

func hostAccessDeniedResponse(server *Server, tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest, message string) controlapipkg.CapabilityResponse {
	if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
		"request_id":           capabilityRequest.RequestID,
		"capability":           capabilityRequest.Capability,
		"reason":               secrets.RedactText(message),
		"denial_code":          controlapipkg.DenialCodePolicyDenied,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}); err != nil {
		return auditUnavailableCapabilityResponse(capabilityRequest.RequestID)
	}
	resp := controlapipkg.CapabilityResponse{
		RequestID:    capabilityRequest.RequestID,
		Status:       controlapipkg.ResponseStatusDenied,
		DenialReason: message,
		DenialCode:   controlapipkg.DenialCodePolicyDenied,
	}
	server.emitUIToolDenied(tokenClaims.ControlSessionID, capabilityRequest, controlapipkg.DenialCodePolicyDenied, message)
	return resp
}

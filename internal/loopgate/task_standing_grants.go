package loopgate

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"morph/internal/config"
)

const (
	TaskExecutionClassApprovalRequired       = "approval_required"
	TaskExecutionClassLocalWorkspaceOrganize = "local_workspace_organize"
	TaskExecutionClassLocalDesktopOrganize   = "local_desktop_organize"

	taskStandingGrantConfigSection = "task_standing_grants"
	taskStandingGrantConfigVersion = "1"
)

type taskExecutionClassDefinition struct {
	Class         string
	Label         string
	Description   string
	SandboxOnly   bool
	StandingGrant bool
	DefaultGrant  bool
}

type taskStandingGrantConfigFile struct {
	Version        string   `json:"version"`
	GrantedClasses []string `json:"granted_classes,omitempty"`
}

var taskExecutionClassCatalog = map[string]taskExecutionClassDefinition{
	TaskExecutionClassApprovalRequired: {
		Class:       TaskExecutionClassApprovalRequired,
		Label:       "Ask Before Running",
		Description: "Use Loopgate approval before starting this task.",
	},
	TaskExecutionClassLocalWorkspaceOrganize: {
		Class:         TaskExecutionClassLocalWorkspaceOrganize,
		Label:         "Organize Haven Files",
		Description:   "Rearrange or tidy files inside Morph's own Haven workspace only.",
		SandboxOnly:   true,
		StandingGrant: true,
		DefaultGrant:  true,
	},
	TaskExecutionClassLocalDesktopOrganize: {
		Class:         TaskExecutionClassLocalDesktopOrganize,
		Label:         "Organize Haven Desktop",
		Description:   "Rearrange desktop items and layout inside Haven only.",
		SandboxOnly:   true,
		StandingGrant: true,
		DefaultGrant:  true,
	},
}

func defaultTaskStandingGrantConfig() taskStandingGrantConfigFile {
	grantedClasses := make([]string, 0, len(taskExecutionClassCatalog))
	for _, classDefinition := range taskExecutionClassCatalog {
		if classDefinition.StandingGrant && classDefinition.DefaultGrant {
			grantedClasses = append(grantedClasses, classDefinition.Class)
		}
	}
	sort.Strings(grantedClasses)
	return taskStandingGrantConfigFile{
		Version:        taskStandingGrantConfigVersion,
		GrantedClasses: grantedClasses,
	}
}

func normalizeTaskExecutionClass(rawExecutionClass string) string {
	normalizedExecutionClass := strings.TrimSpace(strings.ToLower(rawExecutionClass))
	if normalizedExecutionClass == "" {
		return TaskExecutionClassApprovalRequired
	}
	return normalizedExecutionClass
}

func validateTaskExecutionClass(executionClass string) error {
	classDefinition, found := taskExecutionClassCatalog[executionClass]
	if !found {
		return fmt.Errorf("execution_class %q is invalid", executionClass)
	}
	if strings.TrimSpace(classDefinition.Class) == "" {
		return fmt.Errorf("execution_class %q is invalid", executionClass)
	}
	return nil
}

func taskExecutionClassLabel(executionClass string) string {
	classDefinition, found := taskExecutionClassCatalog[normalizeTaskExecutionClass(executionClass)]
	if !found {
		return ""
	}
	return classDefinition.Label
}

func taskExecutionClassDescription(executionClass string) string {
	classDefinition, found := taskExecutionClassCatalog[normalizeTaskExecutionClass(executionClass)]
	if !found {
		return ""
	}
	return classDefinition.Description
}

func taskExecutionClassSandboxOnly(executionClass string) bool {
	classDefinition, found := taskExecutionClassCatalog[normalizeTaskExecutionClass(executionClass)]
	if !found {
		return false
	}
	return classDefinition.SandboxOnly
}

func taskExecutionClassRequiresApproval(executionClass string, grantedStandingClasses map[string]bool) bool {
	normalizedExecutionClass := normalizeTaskExecutionClass(executionClass)
	classDefinition, found := taskExecutionClassCatalog[normalizedExecutionClass]
	if !found {
		return true
	}
	if !classDefinition.StandingGrant {
		return true
	}
	return !grantedStandingClasses[normalizedExecutionClass]
}

func (server *Server) handleTaskStandingGrants(writer http.ResponseWriter, request *http.Request) {
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}

	switch request.Method {
	case http.MethodGet:
		if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
			server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		statusResponse, err := server.taskStandingGrantStatus()
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
		requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
		if !verified {
			server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		var updateRequest TaskStandingGrantUpdateRequest
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
		statusResponse, err := server.updateTaskStandingGrant(tokenClaims, updateRequest)
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

func (server *Server) taskStandingGrantStatus() (TaskStandingGrantStatusResponse, error) {
	grantedStandingClasses, err := server.loadTaskStandingGrantSet()
	if err != nil {
		return TaskStandingGrantStatusResponse{}, err
	}

	classNames := make([]string, 0, len(taskExecutionClassCatalog))
	for className, classDefinition := range taskExecutionClassCatalog {
		if !classDefinition.StandingGrant {
			continue
		}
		classNames = append(classNames, className)
	}
	sort.Strings(classNames)

	grantStatuses := make([]TaskStandingGrantStatus, 0, len(classNames))
	for _, className := range classNames {
		classDefinition := taskExecutionClassCatalog[className]
		grantStatuses = append(grantStatuses, TaskStandingGrantStatus{
			Class:        classDefinition.Class,
			Label:        classDefinition.Label,
			Description:  classDefinition.Description,
			SandboxOnly:  classDefinition.SandboxOnly,
			DefaultGrant: classDefinition.DefaultGrant,
			Granted:      grantedStandingClasses[classDefinition.Class],
		})
	}
	return TaskStandingGrantStatusResponse{Grants: grantStatuses}, nil
}

func (server *Server) updateTaskStandingGrant(tokenClaims capabilityToken, updateRequest TaskStandingGrantUpdateRequest) (TaskStandingGrantStatusResponse, error) {
	normalizedExecutionClass := normalizeTaskExecutionClass(updateRequest.Class)
	classDefinition, found := taskExecutionClassCatalog[normalizedExecutionClass]
	if !found || !classDefinition.StandingGrant {
		return TaskStandingGrantStatusResponse{}, fmt.Errorf("task standing grant %q is not configurable", updateRequest.Class)
	}

	grantedStandingClasses, err := server.loadTaskStandingGrantSet()
	if err != nil {
		return TaskStandingGrantStatusResponse{}, err
	}

	if updateRequest.Granted {
		grantedStandingClasses[normalizedExecutionClass] = true
	} else {
		delete(grantedStandingClasses, normalizedExecutionClass)
	}

	if err := server.saveTaskStandingGrantSet(grantedStandingClasses); err != nil {
		return TaskStandingGrantStatusResponse{}, err
	}

	if err := server.logEvent("task.standing_grant.updated", tokenClaims.ControlSessionID, map[string]interface{}{
		"class":                normalizedExecutionClass,
		"granted":              updateRequest.Granted,
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
	}); err != nil {
		return TaskStandingGrantStatusResponse{}, fmt.Errorf("append task standing grant audit event: %w", err)
	}

	return server.taskStandingGrantStatus()
}

func (server *Server) loadTaskStandingGrantSet() (map[string]bool, error) {
	configFile, err := config.LoadOrSeed[taskStandingGrantConfigFile](server.configStateDir, taskStandingGrantConfigSection, "", nil, defaultTaskStandingGrantConfig)
	if err != nil {
		return nil, fmt.Errorf("load task standing grant config: %w", err)
	}

	grantedStandingClasses := make(map[string]bool, len(configFile.GrantedClasses))
	for _, rawGrantedClass := range configFile.GrantedClasses {
		normalizedExecutionClass := normalizeTaskExecutionClass(rawGrantedClass)
		classDefinition, found := taskExecutionClassCatalog[normalizedExecutionClass]
		if !found || !classDefinition.StandingGrant {
			continue
		}
		grantedStandingClasses[normalizedExecutionClass] = true
	}
	return grantedStandingClasses, nil
}

func (server *Server) saveTaskStandingGrantSet(grantedStandingClasses map[string]bool) error {
	grantedClasses := make([]string, 0, len(grantedStandingClasses))
	for grantedClass, granted := range grantedStandingClasses {
		if !granted {
			continue
		}
		classDefinition, found := taskExecutionClassCatalog[grantedClass]
		if !found || !classDefinition.StandingGrant {
			continue
		}
		grantedClasses = append(grantedClasses, grantedClass)
	}
	sort.Strings(grantedClasses)
	if err := config.SaveJSONConfig(server.configStateDir, taskStandingGrantConfigSection, taskStandingGrantConfigFile{
		Version:        taskStandingGrantConfigVersion,
		GrantedClasses: grantedClasses,
	}); err != nil {
		return fmt.Errorf("save task standing grant config: %w", err)
	}
	return nil
}

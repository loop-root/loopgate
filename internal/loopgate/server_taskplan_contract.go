package loopgate

const maxTaskPlanBodyBytes int64 = 64 * 1024

// SubmitTaskPlanRequest is the request envelope for POST /v1/task/plan.
type SubmitTaskPlanRequest struct {
	GoalText      string         `json:"goal_text"`
	Steps         []TaskPlanStep `json:"steps"`
	CanonicalHash string         `json:"canonical_hash"`
}

// SubmitTaskPlanResponse is the response envelope for POST /v1/task/plan.
type SubmitTaskPlanResponse struct {
	PlanID       string `json:"plan_id"`
	Status       string `json:"status"`
	DenialReason string `json:"denial_reason,omitempty"`
	DenialCode   string `json:"denial_code,omitempty"`
}

// RequestTaskLeaseRequest is the request envelope for POST /v1/task/lease.
type RequestTaskLeaseRequest struct {
	PlanID    string `json:"plan_id"`
	StepIndex int    `json:"step_index"`
	PlanHash  string `json:"plan_hash"`
}

// RequestTaskLeaseResponse is the response envelope for POST /v1/task/lease.
type RequestTaskLeaseResponse struct {
	LeaseID      string `json:"lease_id"`
	MorphlingID  string `json:"morphling_id"`
	Capability   string `json:"capability"`
	StagingDir   string `json:"staging_dir"`
	ExpiresAtUTC string `json:"expires_at_utc"`
	Status       string `json:"status"`
	DenialReason string `json:"denial_reason,omitempty"`
	DenialCode   string `json:"denial_code,omitempty"`
}

// ExecuteTaskLeaseRequest is the request envelope for POST /v1/task/execute.
// The caller provides only lease_id and morphling_id. Capability and arguments
// are bound by the lease; Loopgate executes from lease contents only.
type ExecuteTaskLeaseRequest struct {
	LeaseID     string `json:"lease_id"`
	MorphlingID string `json:"morphling_id"`
}

// ExecuteTaskLeaseResponse is the response envelope for POST /v1/task/execute.
type ExecuteTaskLeaseResponse struct {
	Status       string          `json:"status"`
	StepResult   *TaskStepResult `json:"step_result,omitempty"`
	DenialReason string          `json:"denial_reason,omitempty"`
	DenialCode   string          `json:"denial_code,omitempty"`
}

// CompleteTaskLeaseRequest is the request envelope for POST /v1/task/complete.
// This only finalizes lease consumption. Provider output was already staged
// by Loopgate during /v1/task/execute.
type CompleteTaskLeaseRequest struct {
	LeaseID     string `json:"lease_id"`
	MorphlingID string `json:"morphling_id"`
}

// CompleteTaskLeaseResponse is the response envelope for POST /v1/task/complete.
type CompleteTaskLeaseResponse struct {
	Status       string `json:"status"`
	DenialReason string `json:"denial_reason,omitempty"`
	DenialCode   string `json:"denial_code,omitempty"`
}

// TaskPlanResultRequest is the request envelope for POST /v1/task/result.
type TaskPlanResultRequest struct {
	PlanID string `json:"plan_id"`
}

// TaskPlanResultResponse is the response envelope for POST /v1/task/result.
type TaskPlanResultResponse struct {
	PlanID      string          `json:"plan_id"`
	Status      string          `json:"status"`
	GoalText    string          `json:"goal_text"`
	StepResult  *TaskStepResult `json:"step_result,omitempty"`
	ArtifactRef string          `json:"artifact_ref,omitempty"`
}

func taskPlanAuditUnavailableResponse() SubmitTaskPlanResponse {
	return SubmitTaskPlanResponse{
		Status:       ResponseStatusError,
		DenialReason: "control-plane audit is unavailable",
		DenialCode:   DenialCodeAuditUnavailable,
	}
}

func taskLeaseAuditUnavailableResponse() RequestTaskLeaseResponse {
	return RequestTaskLeaseResponse{
		Status:       ResponseStatusError,
		DenialReason: "control-plane audit is unavailable",
		DenialCode:   DenialCodeAuditUnavailable,
	}
}

func taskExecuteAuditUnavailableResponse() ExecuteTaskLeaseResponse {
	return ExecuteTaskLeaseResponse{
		Status:       ResponseStatusError,
		DenialReason: "control-plane audit is unavailable",
		DenialCode:   DenialCodeAuditUnavailable,
	}
}

func taskCompleteAuditUnavailableResponse() CompleteTaskLeaseResponse {
	return CompleteTaskLeaseResponse{
		Status:       ResponseStatusError,
		DenialReason: "control-plane audit is unavailable",
		DenialCode:   DenialCodeAuditUnavailable,
	}
}

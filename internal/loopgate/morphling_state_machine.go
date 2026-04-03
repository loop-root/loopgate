package loopgate

import "fmt"

const (
	morphlingStateRequested            = "requested"
	morphlingStateAuthorizing          = "authorizing"
	morphlingStatePendingSpawnApproval = "pending_spawn_approval"
	morphlingStateSpawned              = "spawned"
	morphlingStateRunning              = "running"
	morphlingStateCompleting           = "completing"
	morphlingStatePendingReview        = "pending_review"
	morphlingStateTerminating          = "terminating"
	morphlingStateTerminated           = "terminated"
)

const (
	morphlingOutcomeApproved  = "approved"
	morphlingOutcomeRejected  = "rejected"
	morphlingOutcomeCancelled = "cancelled"
	morphlingOutcomeFailed    = "failed"
)

const (
	morphlingReasonNormalCompletion      = "normal_completion"
	morphlingReasonReviewTTLExpired      = "review_ttl_expired"
	morphlingReasonSpawnDeniedByOperator = "spawn_denied_by_operator"
	morphlingReasonSpawnApprovalExpired  = "spawn_approval_ttl_expired"
	morphlingReasonOperatorCancelled     = "operator_cancelled"
	morphlingReasonParentSessionEnded    = "parent_session_terminated"
	morphlingReasonExecutionStartFailed  = "execution_start_failed"
	morphlingReasonTimeBudgetExceeded    = "time_budget_exceeded"
	morphlingReasonTokenBudgetExceeded   = "token_budget_exceeded"
	morphlingReasonDiskQuotaExceeded     = "disk_quota_exceeded"
	morphlingReasonCapabilityTokenExpiry = "capability_token_expired"
	morphlingReasonStagingFailed         = "staging_failed"
	morphlingReasonLoopgateRestart       = "loopgate_restart"
)

type morphlingLifecycleEvent string

const (
	morphlingEventBeginAuthorization   morphlingLifecycleEvent = "begin_authorization"
	morphlingEventAwaitSpawnApproval   morphlingLifecycleEvent = "await_spawn_approval"
	morphlingEventSpawnSucceeded       morphlingLifecycleEvent = "spawn_succeeded"
	morphlingEventExecutionStarted     morphlingLifecycleEvent = "execution_started"
	morphlingEventExecutionCompleted   morphlingLifecycleEvent = "execution_completed"
	morphlingEventAwaitReview          morphlingLifecycleEvent = "await_review"
	morphlingEventBeginTermination     morphlingLifecycleEvent = "begin_termination"
	morphlingEventFinishTermination    morphlingLifecycleEvent = "finish_termination"
)

func morphlingNextState(currentState string, lifecycleEvent morphlingLifecycleEvent) (string, error) {
	switch currentState {
	case morphlingStateRequested:
		if lifecycleEvent == morphlingEventBeginAuthorization {
			return morphlingStateAuthorizing, nil
		}
	case morphlingStateAuthorizing:
		switch lifecycleEvent {
		case morphlingEventAwaitSpawnApproval:
			return morphlingStatePendingSpawnApproval, nil
		case morphlingEventSpawnSucceeded:
			return morphlingStateSpawned, nil
		case morphlingEventBeginTermination:
			return morphlingStateTerminating, nil
		}
	case morphlingStatePendingSpawnApproval:
		switch lifecycleEvent {
		case morphlingEventSpawnSucceeded:
			return morphlingStateSpawned, nil
		case morphlingEventBeginTermination:
			return morphlingStateTerminating, nil
		}
	case morphlingStateSpawned:
		switch lifecycleEvent {
		case morphlingEventExecutionStarted:
			return morphlingStateRunning, nil
		case morphlingEventBeginTermination:
			return morphlingStateTerminating, nil
		}
	case morphlingStateRunning:
		switch lifecycleEvent {
		case morphlingEventExecutionCompleted:
			return morphlingStateCompleting, nil
		case morphlingEventBeginTermination:
			return morphlingStateTerminating, nil
		}
	case morphlingStateCompleting:
		switch lifecycleEvent {
		case morphlingEventAwaitReview:
			return morphlingStatePendingReview, nil
		case morphlingEventBeginTermination:
			return morphlingStateTerminating, nil
		}
	case morphlingStatePendingReview:
		if lifecycleEvent == morphlingEventBeginTermination {
			return morphlingStateTerminating, nil
		}
	case morphlingStateTerminating:
		if lifecycleEvent == morphlingEventFinishTermination {
			return morphlingStateTerminated, nil
		}
	case morphlingStateTerminated:
	}

	return "", fmt.Errorf("%w: illegal morphling transition %s --(%s)--> ?", errMorphlingStateInvalid, currentState, lifecycleEvent)
}

func morphlingStateConsumesCapacity(state string) bool {
	switch state {
	case morphlingStateAuthorizing,
		morphlingStatePendingSpawnApproval,
		morphlingStateSpawned,
		morphlingStateRunning,
		morphlingStateCompleting,
		morphlingStatePendingReview:
		return true
	default:
		return false
	}
}


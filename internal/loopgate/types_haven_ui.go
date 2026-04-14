package loopgate

import (
	"fmt"
	"strings"

	"morph/internal/identifiers"
)

// HavenMemoryInventoryResponse is the operator-facing memory inventory projection for GET /v1/ui/memory.
type HavenMemoryInventoryResponse struct {
	WakeStateID             string                   `json:"wake_state_id,omitempty"`
	WakeCreatedAtUTC        string                   `json:"wake_created_at_utc,omitempty"`
	RecentFactCount         int                      `json:"recent_fact_count"`
	ActiveGoalCount         int                      `json:"active_goal_count"`
	UnresolvedItemCount     int                      `json:"unresolved_item_count"`
	ResonateKeyCount        int                      `json:"resonate_key_count"`
	IncludedDiagnosticCount int                      `json:"included_diagnostic_count"`
	ExcludedDiagnosticCount int                      `json:"excluded_diagnostic_count"`
	PendingReviewCount      int                      `json:"pending_review_count"`
	EligibleCount           int                      `json:"eligible_count"`
	TombstonedCount         int                      `json:"tombstoned_count"`
	PurgedCount             int                      `json:"purged_count"`
	Objects                 []HavenMemoryObjectEntry `json:"objects"`
}

// HavenMemoryObjectEntry is one operator-manageable continuity lineage root.
type HavenMemoryObjectEntry struct {
	InspectionID             string `json:"inspection_id"`
	ThreadID                 string `json:"thread_id"`
	Scope                    string `json:"scope"`
	ObjectKind               string `json:"object_kind"`
	Summary                  string `json:"summary,omitempty"`
	SubmittedAtUTC           string `json:"submitted_at_utc,omitempty"`
	CompletedAtUTC           string `json:"completed_at_utc,omitempty"`
	DerivationOutcome        string `json:"derivation_outcome"`
	ReviewStatus             string `json:"review_status"`
	LineageStatus            string `json:"lineage_status"`
	GoalType                 string `json:"goal_type,omitempty"`
	GoalFamilyID             string `json:"goal_family_id,omitempty"`
	DerivedDistillateCount   int    `json:"derived_distillate_count"`
	DerivedResonateKeyCount  int    `json:"derived_resonate_key_count"`
	SupersedesInspectionID   string `json:"supersedes_inspection_id,omitempty"`
	SupersededByInspectionID string `json:"superseded_by_inspection_id,omitempty"`
	RetentionWindowActive    bool   `json:"retention_window_active,omitempty"`
	CanReview                bool   `json:"can_review"`
	CanTombstone             bool   `json:"can_tombstone"`
	CanPurge                 bool   `json:"can_purge"`
}

// HavenMemoryResetRequest is the body for POST /v1/ui/memory/reset.
type HavenMemoryResetRequest struct {
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason,omitempty"`
}

// HavenMemoryResetResponse reports the archived fresh-start reset result.
type HavenMemoryResetResponse struct {
	ResetAtUTC               string `json:"reset_at_utc"`
	ArchiveID                string `json:"archive_id,omitempty"`
	PreviousInspectionCount  int    `json:"previous_inspection_count"`
	PreviousDistillateCount  int    `json:"previous_distillate_count"`
	PreviousResonateKeyCount int    `json:"previous_resonate_key_count"`
	WakeStateID              string `json:"wake_state_id,omitempty"`
}

// HavenDeskNote is the runtime/state/haven_desk_notes.json entry shape.
// The type name is retained for compatibility with existing clients.
type HavenDeskNote struct {
	ID                  string               `json:"id"`
	Kind                string               `json:"kind"`
	Title               string               `json:"title"`
	Body                string               `json:"body"`
	Action              *HavenDeskNoteAction `json:"action,omitempty"`
	ActionExecutedAtUTC string               `json:"action_executed_at_utc,omitempty"`
	ActionThreadID      string               `json:"action_thread_id,omitempty"`
	CreatedAtUTC        string               `json:"created_at_utc"`
	ArchivedAtUTC       string               `json:"archived_at_utc,omitempty"`
}

// HavenDeskNoteAction is the desk-note action shape for UI consumers.
type HavenDeskNoteAction struct {
	Kind    string `json:"kind"`
	Label   string `json:"label,omitempty"`
	Message string `json:"message,omitempty"`
}

// HavenDeskNotesResponse is the body for GET /v1/ui/desk-notes.
type HavenDeskNotesResponse struct {
	Notes []HavenDeskNote `json:"notes"`
}

// HavenDeskNoteDismissRequest is the body for POST /v1/ui/desk-notes/dismiss.
type HavenDeskNoteDismissRequest struct {
	NoteID string `json:"note_id"`
}

// HavenJournalEntrySummary is one journal entry row for local UI consumers.
type HavenJournalEntrySummary struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Preview      string `json:"preview"`
	UpdatedAtUTC string `json:"updated_at_utc"`
	EntryCount   int    `json:"entry_count"`
}

// HavenJournalEntriesResponse is GET /v1/ui/journal/entries.
type HavenJournalEntriesResponse struct {
	Entries []HavenJournalEntrySummary `json:"entries"`
}

// HavenJournalEntryResponse is GET /v1/ui/journal/entry.
type HavenJournalEntryResponse struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	EntryCount   int    `json:"entry_count"`
	UpdatedAtUTC string `json:"updated_at_utc,omitempty"`
	Error        string `json:"error,omitempty"`
}

// HavenWorkingNoteSummary is one working-note row for local UI consumers.
type HavenWorkingNoteSummary struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Preview      string `json:"preview"`
	UpdatedAtUTC string `json:"updated_at_utc"`
}

// HavenWorkingNotesResponse is GET /v1/ui/working-notes.
type HavenWorkingNotesResponse struct {
	Notes []HavenWorkingNoteSummary `json:"notes"`
}

// HavenWorkingNoteResponse is GET /v1/ui/working-notes/entry.
type HavenWorkingNoteResponse struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

// HavenWorkingNoteSaveRequest is POST /v1/ui/working-notes/save.
type HavenWorkingNoteSaveRequest struct {
	Path    string `json:"path,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
}

// HavenWorkingNoteSaveResponse is the save response for local UI consumers.
type HavenWorkingNoteSaveResponse struct {
	Saved bool   `json:"saved"`
	Path  string `json:"path,omitempty"`
	Title string `json:"title,omitempty"`
	Error string `json:"error,omitempty"`
}

// HavenWorkspaceListRequest is POST /v1/ui/workspace/list.
type HavenWorkspaceListRequest struct {
	Path string `json:"path"`
}

// HavenWorkspaceListEntry is one workspace row for local UI consumers.
type HavenWorkspaceListEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	EntryType  string `json:"entry_type"`
	SizeBytes  int64  `json:"size_bytes"`
	ModTimeUTC string `json:"mod_time_utc,omitempty"`
}

// HavenWorkspaceListResponse is the workspace listing response for local UI consumers.
type HavenWorkspaceListResponse struct {
	Path    string                    `json:"path"`
	Entries []HavenWorkspaceListEntry `json:"entries"`
	Error   string                    `json:"error,omitempty"`
}

// HavenWorkspaceHostLayoutResponse is GET /v1/ui/workspace/host-layout — resolved
// host filesystem locations for primary sandbox dirs (operator convenience).
type HavenWorkspaceHostLayoutResponse struct {
	ProjectsHostPath string `json:"projects_host_path,omitempty"`
	ResearchHostPath string `json:"research_host_path,omitempty"`
	Error            string `json:"error,omitempty"`
}

// HavenWorkspacePreviewRequest is POST /v1/ui/workspace/preview.
type HavenWorkspacePreviewRequest struct {
	Path string `json:"path"`
}

// HavenWorkspacePreviewResponse is the workspace preview response for local UI consumers.
type HavenWorkspacePreviewResponse struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Path      string `json:"path"`
	Error     string `json:"error,omitempty"`
}

// HavenPresenceResponse is the presence projection for GET /v1/ui/presence.
type HavenPresenceResponse struct {
	State      string `json:"state"`
	StatusText string `json:"status_text"`
	DetailText string `json:"detail_text,omitempty"`
	Anchor     string `json:"anchor"`
}

// HavenMorphSleepResponse extends presence with booleans for light clients (GET /v1/ui/morph-sleep).
type HavenMorphSleepResponse struct {
	State      string `json:"state"`
	StatusText string `json:"status_text"`
	DetailText string `json:"detail_text,omitempty"`
	Anchor     string `json:"anchor"`
	IsSleeping bool   `json:"is_sleeping"`
	IsResting  bool   `json:"is_resting"`
}

func (havenMemoryResetRequest HavenMemoryResetRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("operation_id", strings.TrimSpace(havenMemoryResetRequest.OperationID)); err != nil {
		return err
	}
	if len(strings.TrimSpace(havenMemoryResetRequest.Reason)) > 200 {
		return fmt.Errorf("reason exceeds maximum length")
	}
	return nil
}

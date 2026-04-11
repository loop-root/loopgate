package loopgate

const explicitTodoSourceKind = "explicit_todo_item"

const (
	todoItemOpStatusSet = "status_set"

	explicitTodoWorkflowStatusTodo       = "todo"
	explicitTodoWorkflowStatusInProgress = "in_progress"
	explicitTodoWorkflowStatusDone       = "done"

	maxUIRecentCompletedTodoItems = 20
)

const (
	taskKindCarryOver = "carry_over"
	taskKindOneOff    = "one_off"
	taskKindScheduled = "scheduled"

	taskSourceUser         = "user"
	taskSourceContinuity   = "continuity"
	taskFactKind           = "task.kind"
	taskFactSourceKind     = "task.source_kind"
	taskFactNextStep       = "task.next_step"
	taskFactScheduledForUT = "task.scheduled_for_utc"
	taskFactExecutionClass = "task.execution_class"
)

type explicitTodoItemRecord struct {
	InspectionID    string
	DistillateID    string
	ResonateKeyID   string
	ItemID          string
	Text            string
	TaskKind        string
	SourceKind      string
	NextStep        string
	ScheduledForUTC string
	ExecutionClass  string
	CreatedAtUTC    string
	// Status is "todo" or "in_progress" for open items (default "todo").
	Status string
}

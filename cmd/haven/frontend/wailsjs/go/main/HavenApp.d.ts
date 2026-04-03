export interface ThreadSummary {
  thread_id: string;
  title: string;
  folder?: string;
  created_at: string;
  updated_at: string;
  event_count: number;
}

export interface MemoryWakeStateOpenItem {
  id: string;
  text: string;
  task_kind?: string;
  source_kind?: string;
  next_step?: string;
  scheduled_for_utc?: string;
  execution_class?: string;
  created_at_utc?: string;
}

export interface ConversationEvent {
  v: string;
  ts: string;
  thread_id: string;
  type: string;
  data?: Record<string, unknown>;
}

export interface ChatResponse {
  thread_id: string;
  accepted: boolean;
  reason?: string;
}

export function NewThread(): Promise<ThreadSummary>;
export function ListThreads(): Promise<ThreadSummary[]>;
export function ListFolders(): Promise<string[]>;
export function SetThreadFolder(
  threadID: string,
  folder: string
): Promise<void>;
export function RenameThread(
  threadID: string,
  title: string
): Promise<void>;
export function LoadThread(threadID: string): Promise<ConversationEvent[]>;
export function SendMessage(
  threadID: string,
  message: string
): Promise<ChatResponse>;
export function CancelExecution(threadID: string): Promise<void>;
export function DecideApproval(
  threadID: string,
  approvalRequestID: string,
  approved: boolean
): Promise<void>;
export function GetExecutionState(threadID: string): Promise<string>;

export interface WorkspaceListEntry {
  name: string;
  entry_type: string;
  size_bytes: number;
  mod_time_utc: string;
}

export interface WorkspaceListResponse {
  path: string;
  entries: WorkspaceListEntry[];
}

export interface WorkspaceImportResponse {
  imported: boolean;
  name?: string;
  path?: string;
  error?: string;
}

export interface WorkspaceExportResponse {
  exported: boolean;
  host_path?: string;
  error?: string;
}

export interface WorkspaceRestoreResponse {
  restored: boolean;
  error?: string;
}

export interface WorkspacePreviewResponse {
  content: string;
  truncated: boolean;
  path: string;
  error?: string;
}

export function WorkspaceList(path: string): Promise<WorkspaceListResponse>;
export function WorkspaceImportFile(): Promise<WorkspaceImportResponse>;
export function WorkspaceImportDirectory(): Promise<WorkspaceImportResponse>;
export function WorkspaceImportPath(
  hostPath: string
): Promise<WorkspaceImportResponse>;
export function WorkspaceExport(
  sandboxPath: string
): Promise<WorkspaceExportResponse>;
export function WorkspaceRestoreOriginal(
  havenPath: string
): Promise<WorkspaceRestoreResponse>;
export function WorkspacePreviewFile(
  path: string
): Promise<WorkspacePreviewResponse>;

export interface WorkspaceWriteResponse {
  written: boolean;
  error?: string;
}

export interface WorkspaceCreateDirResponse {
  created: boolean;
  error?: string;
}

export interface WorkspaceDeleteResponse {
  deleted: boolean;
  error?: string;
}

export interface WorkspaceRenameResponse {
  renamed: boolean;
  error?: string;
}

export interface SharedFolderStatusResponse {
  name: string;
  host_path: string;
  sandbox_relative_path: string;
  sandbox_absolute_path: string;
  host_exists: boolean;
  mirror_ready: boolean;
  entry_count: number;
}

export function WorkspaceWriteFile(
  path: string,
  content: string
): Promise<WorkspaceWriteResponse>;
export function WorkspaceCreateDir(path: string): Promise<WorkspaceCreateDirResponse>;
export function WorkspaceDelete(path: string): Promise<WorkspaceDeleteResponse>;
export function WorkspaceRename(
  oldPath: string,
  newName: string
): Promise<WorkspaceRenameResponse>;

// --- Desktop: System Status & Security ---

export interface WorkerSummary {
  id: string;
  class: string;
  state: string;
  goal?: string;
  artifact_count: number;
  pending_review: boolean;
  capability_count: number;
  time_budget_seconds?: number;
}

export interface CapabilityAccess {
  name: string;
  category: string;
  operation: string;
  granted: boolean;
}

export interface PolicyOverview {
  read_enabled: boolean;
  write_enabled: boolean;
  write_requires_approval: boolean;
}

export interface SystemStatusResponse {
  turn_count: number;
  pending_approvals: number;
  active_workers: number;
  max_workers: number;
  workers: WorkerSummary[];
  capabilities: CapabilityAccess[];
  policy: PolicyOverview;
  timestamp: string;
  error?: string;
}

export interface ConnectionSummary {
  provider: string;
  status: string;
  last_validated?: string;
}

export interface ApprovalSummary {
  approval_request_id: string;
  capability: string;
  expires_at: string;
  redacted: boolean;
}

export interface StandingTaskGrantSummary {
  class: string;
  label: string;
  description: string;
  sandbox_only: boolean;
  default_grant: boolean;
  granted: boolean;
}

export interface SecurityOverviewResponse {
  capabilities: CapabilityAccess[];
  connections: ConnectionSummary[];
  pending_approvals: ApprovalSummary[];
  standing_task_grants: StandingTaskGrantSummary[];
  policy: PolicyOverview;
  active_morphlings: number;
  error?: string;
}

export function SystemStatus(): Promise<SystemStatusResponse>;
export function SecurityOverview(): Promise<SecurityOverviewResponse>;
export function UpdateTaskStandingGrant(className: string, granted: boolean): Promise<SecurityOverviewResponse>;
export function GetSharedFolderStatus(): Promise<SharedFolderStatusResponse>;
export function SyncSharedFolder(): Promise<SharedFolderStatusResponse>;

// --- Setup Wizard ---

export interface SetupStatus {
  needs_setup: boolean;
  repo_root: string;
}

export interface SetupRequest {
  provider_name: string;
  model_name: string;
  api_key: string;
  base_url: string;
  morph_name: string;
  wallpaper: string;
  granted_folder_ids: string[];
  ambient_enabled: boolean;
  run_in_background: boolean;
}

export interface SetupResponse {
  success: boolean;
  error?: string;
}

export function CheckSetup(): Promise<SetupStatus>;
export function DetectOllama(): Promise<boolean>;
export function ListLocalModels(baseURL: string): Promise<string[]>;
export function CompleteSetup(req: SetupRequest): Promise<SetupResponse>;

// --- Diff View ---

export interface DiffLine {
  type: string;  // "context" | "add" | "remove" | "header"
  text: string;
}

export interface DiffResponse {
  path: string;
  has_changes: boolean;
  lines: DiffLine[];
  error?: string;
}

export function WorkspaceDiff(havenPath: string): Promise<DiffResponse>;

// --- Journal ---

export interface JournalEntrySummary {
  path: string;
  title: string;
  preview: string;
  updated_at_utc: string;
  entry_count: number;
}

export interface JournalEntryResponse {
  path: string;
  title: string;
  content: string;
  entry_count: number;
  error?: string;
}

export function ListJournalEntries(): Promise<JournalEntrySummary[]>;
export function ReadJournalEntry(havenPath: string): Promise<JournalEntryResponse>;

// --- Paint ---

export interface PaintPoint {
  x: number;
  y: number;
}

export interface PaintStroke {
  color: string;
  width: number;
  points: PaintPoint[];
}

export interface PaintSaveRequest {
  title: string;
  width: number;
  height: number;
  background: string;
  strokes: PaintStroke[];
}

export interface PaintSaveResponse {
  saved: boolean;
  path?: string;
  title?: string;
  error?: string;
}

export interface PaintArtworkSummary {
  path: string;
  title: string;
  updated_at_utc: string;
  preview_svg: string;
}

export function ListPaintings(): Promise<PaintArtworkSummary[]>;
export function PaintSaveArtwork(request: PaintSaveRequest): Promise<PaintSaveResponse>;

// --- Memory ---

export interface RememberedFactSummary {
  name: string;
  value: string;
}

export interface MemoryStatusResponse {
  has_wake_state: boolean;
  wake_state_summary: string;
  current_focus?: string;
  remembered_fact_count: number;
  remembered_facts?: RememberedFactSummary[];
  active_goal_count: number;
  unresolved_item_count: number;
  active_goals?: string[];
  unresolved_items?: MemoryWakeStateOpenItem[];
  included_diagnostic_count: number;
  excluded_diagnostic_count: number;
  diagnostic_summary?: string;
  last_updated_utc?: string;
}

export interface TodoActionResponse {
  applied: boolean;
  item_id?: string;
  error?: string;
}

export interface TaskDraft {
  text: string;
  next_step?: string;
  scheduled_for_utc?: string;
  execution_class?: string;
}

export interface WorkingNoteSummary {
  path: string;
  title: string;
  preview: string;
  updated_at_utc: string;
}

export interface WorkingNoteResponse {
  path: string;
  title: string;
  content: string;
  updated_at_utc?: string;
  error?: string;
}

export interface WorkingNoteSaveRequest {
  path?: string;
  title?: string;
  content: string;
}

export interface WorkingNoteSaveResponse {
  saved: boolean;
  path?: string;
  title?: string;
  error?: string;
}

export function AddTask(request: TaskDraft): Promise<TodoActionResponse>;
export function AddTodo(text: string): Promise<TodoActionResponse>;
export function CompleteTodo(itemID: string): Promise<TodoActionResponse>;
export function GetMemoryStatus(): Promise<MemoryStatusResponse>;
export function ListWorkingNotes(): Promise<WorkingNoteSummary[]>;
export function ReadWorkingNote(havenPath: string): Promise<WorkingNoteResponse>;
export function SaveWorkingNote(request: WorkingNoteSaveRequest): Promise<WorkingNoteSaveResponse>;

// --- Presence ---

export interface PresenceResponse {
  state: string;      // "idle" | "working" | "thinking" | "creating" | "reading" | "sleeping" | "excited"
  status_text: string;
  detail_text?: string;
  anchor: string;
}

export function GetPresence(): Promise<PresenceResponse>;

// --- Settings ---

export interface HavenSettings {
  morph_name: string;
  wallpaper: string;
  idle_enabled: boolean;
  ambient_enabled: boolean;
}

export interface SaveSettingsResult {
  success: boolean;
  error?: string;
}

export function GetSettings(): Promise<HavenSettings>;
export function SaveSettings(req: HavenSettings): Promise<SaveSettingsResult>;

export interface LoopgateDiagnosticLoggingStatus {
  enabled: boolean;
  default_level: string;
  log_directory_host_path: string;
  override_active: boolean;
  config_load_error?: string;
}

export function GetLoopgateDiagnosticLogging(): Promise<LoopgateDiagnosticLoggingStatus>;
export function SaveLoopgateDiagnosticLogging(
  enabled: boolean,
  defaultLevel: string
): Promise<SaveSettingsResult>;
export function ClearLoopgateDiagnosticLoggingOverride(): Promise<SaveSettingsResult>;

export interface OllamaModel {
  name: string;
  size: number;
}

export interface ModelSettingsResponse {
  current_model: string;
  provider_name: string;
  base_url: string;
  available_models: OllamaModel[];
  mode: string;
  has_cloud_credential: boolean;
  local_base_url: string;
}

export interface SaveModelRequest {
  model_name: string;
}

export interface SaveModelProviderRequest {
  mode: string;
  model_name: string;
  local_base_url: string;
  anthropic_api_key: string;
}

export function GetModelSettings(): Promise<ModelSettingsResponse>;
export function SaveModelSelection(req: SaveModelRequest): Promise<SaveSettingsResult>;
export function SaveModelProviderSettings(
  req: SaveModelProviderRequest
): Promise<SaveSettingsResult>;

// --- Desk Notes ---

export interface DeskNote {
  id: string;
  kind: string;
  title: string;
  body: string;
  action?: DeskNoteAction;
  action_executed_at_utc?: string;
  action_thread_id?: string;
  created_at_utc: string;
  archived_at_utc?: string;
}

export interface DeskNoteAction {
  kind: string;
  label?: string;
  message?: string;
}

export interface DeskNoteActionResponse {
  success: boolean;
  error?: string;
  thread_id?: string;
}

export function ListDeskNotes(): Promise<DeskNote[]>;
export function DismissDeskNote(noteID: string): Promise<DeskNoteActionResponse>;
export function ExecuteDeskNoteAction(noteID: string): Promise<DeskNoteActionResponse>;

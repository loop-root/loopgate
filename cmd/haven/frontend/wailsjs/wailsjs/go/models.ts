export namespace loopgate {
	
	export class MemoryWakeStateOpenItem {
	    id: string;
	    text: string;
	    task_kind?: string;
	    source_kind?: string;
	    next_step?: string;
	    scheduled_for_utc?: string;
	    execution_class?: string;
	    created_at_utc?: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryWakeStateOpenItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.text = source["text"];
	        this.task_kind = source["task_kind"];
	        this.source_kind = source["source_kind"];
	        this.next_step = source["next_step"];
	        this.scheduled_for_utc = source["scheduled_for_utc"];
	        this.execution_class = source["execution_class"];
	        this.created_at_utc = source["created_at_utc"];
	    }
	}
	export class SharedFolderStatusResponse {
	    name: string;
	    host_path: string;
	    sandbox_relative_path: string;
	    sandbox_absolute_path: string;
	    host_exists: boolean;
	    mirror_ready: boolean;
	    entry_count: number;
	
	    static createFrom(source: any = {}) {
	        return new SharedFolderStatusResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.host_path = source["host_path"];
	        this.sandbox_relative_path = source["sandbox_relative_path"];
	        this.sandbox_absolute_path = source["sandbox_absolute_path"];
	        this.host_exists = source["host_exists"];
	        this.mirror_ready = source["mirror_ready"];
	        this.entry_count = source["entry_count"];
	    }
	}

}

export namespace main {
	
	export class PolicyOverview {
	    read_enabled: boolean;
	    write_enabled: boolean;
	    write_requires_approval: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PolicyOverview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.read_enabled = source["read_enabled"];
	        this.write_enabled = source["write_enabled"];
	        this.write_requires_approval = source["write_requires_approval"];
	    }
	}
	export class CapabilityAccess {
	    name: string;
	    category: string;
	    operation: string;
	    granted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CapabilityAccess(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.category = source["category"];
	        this.operation = source["operation"];
	        this.granted = source["granted"];
	    }
	}
	export class WorkerSummary {
	    id: string;
	    class: string;
	    state: string;
	    goal?: string;
	    artifact_count: number;
	    pending_review: boolean;
	    capability_count: number;
	    time_budget_seconds?: number;
	
	    static createFrom(source: any = {}) {
	        return new WorkerSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.class = source["class"];
	        this.state = source["state"];
	        this.goal = source["goal"];
	        this.artifact_count = source["artifact_count"];
	        this.pending_review = source["pending_review"];
	        this.capability_count = source["capability_count"];
	        this.time_budget_seconds = source["time_budget_seconds"];
	    }
	}
	export class SystemStatusResponse {
	    turn_count: number;
	    pending_approvals: number;
	    active_workers: number;
	    max_workers: number;
	    workers: WorkerSummary[];
	    capabilities: CapabilityAccess[];
	    policy: PolicyOverview;
	    timestamp: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SystemStatusResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.turn_count = source["turn_count"];
	        this.pending_approvals = source["pending_approvals"];
	        this.active_workers = source["active_workers"];
	        this.max_workers = source["max_workers"];
	        this.workers = this.convertValues(source["workers"], WorkerSummary);
	        this.capabilities = this.convertValues(source["capabilities"], CapabilityAccess);
	        this.policy = this.convertValues(source["policy"], PolicyOverview);
	        this.timestamp = source["timestamp"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ApprovalSummary {
	    approval_request_id: string;
	    capability: string;
	    expires_at: string;
	    redacted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ApprovalSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.approval_request_id = source["approval_request_id"];
	        this.capability = source["capability"];
	        this.expires_at = source["expires_at"];
	        this.redacted = source["redacted"];
	    }
	}
	
	export class ChatResponse {
	    thread_id: string;
	    accepted: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.thread_id = source["thread_id"];
	        this.accepted = source["accepted"];
	        this.reason = source["reason"];
	    }
	}
	export class ConnectionSummary {
	    provider: string;
	    status: string;
	    last_validated?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.status = source["status"];
	        this.last_validated = source["last_validated"];
	    }
	}
	export class DeskNoteAction {
	    kind: string;
	    label?: string;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new DeskNoteAction(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.message = source["message"];
	    }
	}
	export class DeskNote {
	    id: string;
	    kind: string;
	    title: string;
	    body: string;
	    action?: DeskNoteAction;
	    action_executed_at_utc?: string;
	    action_thread_id?: string;
	    created_at_utc: string;
	    archived_at_utc?: string;
	
	    static createFrom(source: any = {}) {
	        return new DeskNote(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.body = source["body"];
	        this.action = this.convertValues(source["action"], DeskNoteAction);
	        this.action_executed_at_utc = source["action_executed_at_utc"];
	        this.action_thread_id = source["action_thread_id"];
	        this.created_at_utc = source["created_at_utc"];
	        this.archived_at_utc = source["archived_at_utc"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class DeskNoteActionResponse {
	    success: boolean;
	    error?: string;
	    thread_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new DeskNoteActionResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.error = source["error"];
	        this.thread_id = source["thread_id"];
	    }
	}
	export class DiffLine {
	    type: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new DiffLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.text = source["text"];
	    }
	}
	export class DiffResponse {
	    path: string;
	    has_changes: boolean;
	    lines: DiffLine[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DiffResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.has_changes = source["has_changes"];
	        this.lines = this.convertValues(source["lines"], DiffLine);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HavenSettings {
	    morph_name: string;
	    wallpaper: string;
	    idle_enabled: boolean;
	    ambient_enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HavenSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.morph_name = source["morph_name"];
	        this.wallpaper = source["wallpaper"];
	        this.idle_enabled = source["idle_enabled"];
	        this.ambient_enabled = source["ambient_enabled"];
	    }
	}
	export class IconPosition {
	    x: number;
	    y: number;
	
	    static createFrom(source: any = {}) {
	        return new IconPosition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class IconPositionsResponse {
	    positions: Record<string, IconPosition>;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new IconPositionsResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.positions = this.convertValues(source["positions"], IconPosition, true);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JournalEntryResponse {
	    path: string;
	    title: string;
	    content: string;
	    entry_count: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new JournalEntryResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.entry_count = source["entry_count"];
	        this.error = source["error"];
	    }
	}
	export class JournalEntrySummary {
	    path: string;
	    title: string;
	    preview: string;
	    updated_at_utc: string;
	    entry_count: number;
	
	    static createFrom(source: any = {}) {
	        return new JournalEntrySummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.title = source["title"];
	        this.preview = source["preview"];
	        this.updated_at_utc = source["updated_at_utc"];
	        this.entry_count = source["entry_count"];
	    }
	}
	export class RememberedFactSummary {
	    name: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new RememberedFactSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	    }
	}
	export class MemoryStatusResponse {
	    has_wake_state: boolean;
	    wake_state_summary: string;
	    current_focus?: string;
	    remembered_fact_count: number;
	    remembered_facts?: RememberedFactSummary[];
	    active_goal_count: number;
	    unresolved_item_count: number;
	    active_goals?: string[];
	    unresolved_items?: loopgate.MemoryWakeStateOpenItem[];
	    included_diagnostic_count: number;
	    excluded_diagnostic_count: number;
	    diagnostic_summary?: string;
	    last_updated_utc?: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryStatusResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.has_wake_state = source["has_wake_state"];
	        this.wake_state_summary = source["wake_state_summary"];
	        this.current_focus = source["current_focus"];
	        this.remembered_fact_count = source["remembered_fact_count"];
	        this.remembered_facts = this.convertValues(source["remembered_facts"], RememberedFactSummary);
	        this.active_goal_count = source["active_goal_count"];
	        this.unresolved_item_count = source["unresolved_item_count"];
	        this.active_goals = source["active_goals"];
	        this.unresolved_items = this.convertValues(source["unresolved_items"], loopgate.MemoryWakeStateOpenItem);
	        this.included_diagnostic_count = source["included_diagnostic_count"];
	        this.excluded_diagnostic_count = source["excluded_diagnostic_count"];
	        this.diagnostic_summary = source["diagnostic_summary"];
	        this.last_updated_utc = source["last_updated_utc"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OllamaModel {
	    name: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new OllamaModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	    }
	}
	export class ModelSettingsResponse {
	    current_model: string;
	    provider_name: string;
	    base_url: string;
	    available_models: OllamaModel[];
	
	    static createFrom(source: any = {}) {
	        return new ModelSettingsResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current_model = source["current_model"];
	        this.provider_name = source["provider_name"];
	        this.base_url = source["base_url"];
	        this.available_models = this.convertValues(source["available_models"], OllamaModel);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class PaintArtworkSummary {
	    path: string;
	    title: string;
	    updated_at_utc: string;
	    preview_svg: string;
	
	    static createFrom(source: any = {}) {
	        return new PaintArtworkSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.title = source["title"];
	        this.updated_at_utc = source["updated_at_utc"];
	        this.preview_svg = source["preview_svg"];
	    }
	}
	export class PaintPoint {
	    x: number;
	    y: number;
	
	    static createFrom(source: any = {}) {
	        return new PaintPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class PaintStroke {
	    color: string;
	    width: number;
	    points: PaintPoint[];
	
	    static createFrom(source: any = {}) {
	        return new PaintStroke(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.color = source["color"];
	        this.width = source["width"];
	        this.points = this.convertValues(source["points"], PaintPoint);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PaintSaveRequest {
	    title: string;
	    width: number;
	    height: number;
	    background: string;
	    strokes: PaintStroke[];
	
	    static createFrom(source: any = {}) {
	        return new PaintSaveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.background = source["background"];
	        this.strokes = this.convertValues(source["strokes"], PaintStroke);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PaintSaveResponse {
	    saved: boolean;
	    path?: string;
	    title?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new PaintSaveResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saved = source["saved"];
	        this.path = source["path"];
	        this.title = source["title"];
	        this.error = source["error"];
	    }
	}
	
	
	export class PresenceResponse {
	    state: string;
	    status_text: string;
	    detail_text?: string;
	    anchor: string;
	
	    static createFrom(source: any = {}) {
	        return new PresenceResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.status_text = source["status_text"];
	        this.detail_text = source["detail_text"];
	        this.anchor = source["anchor"];
	    }
	}
	
	export class SaveModelRequest {
	    model_name: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveModelRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model_name = source["model_name"];
	    }
	}
	export class SaveSettingsResult {
	    success: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveSettingsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.error = source["error"];
	    }
	}
	export class StandingTaskGrantSummary {
	    class: string;
	    label: string;
	    description: string;
	    sandbox_only: boolean;
	    default_grant: boolean;
	    granted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StandingTaskGrantSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.class = source["class"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.sandbox_only = source["sandbox_only"];
	        this.default_grant = source["default_grant"];
	        this.granted = source["granted"];
	    }
	}
	export class SecurityOverviewResponse {
	    capabilities: CapabilityAccess[];
	    connections: ConnectionSummary[];
	    pending_approvals: ApprovalSummary[];
	    standing_task_grants?: StandingTaskGrantSummary[];
	    policy: PolicyOverview;
	    active_morphlings: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SecurityOverviewResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.capabilities = this.convertValues(source["capabilities"], CapabilityAccess);
	        this.connections = this.convertValues(source["connections"], ConnectionSummary);
	        this.pending_approvals = this.convertValues(source["pending_approvals"], ApprovalSummary);
	        this.standing_task_grants = this.convertValues(source["standing_task_grants"], StandingTaskGrantSummary);
	        this.policy = this.convertValues(source["policy"], PolicyOverview);
	        this.active_morphlings = source["active_morphlings"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SetupRequest {
	    provider_name: string;
	    model_name: string;
	    api_key: string;
	    base_url: string;
	    morph_name: string;
	    wallpaper: string;
	    granted_folder_ids: string[];
	    ambient_enabled: boolean;
	    run_in_background: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SetupRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider_name = source["provider_name"];
	        this.model_name = source["model_name"];
	        this.api_key = source["api_key"];
	        this.base_url = source["base_url"];
	        this.morph_name = source["morph_name"];
	        this.wallpaper = source["wallpaper"];
	        this.granted_folder_ids = source["granted_folder_ids"];
	        this.ambient_enabled = source["ambient_enabled"];
	        this.run_in_background = source["run_in_background"];
	    }
	}
	export class SetupResponse {
	    success: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SetupResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.error = source["error"];
	    }
	}
	export class SetupStatus {
	    needs_setup: boolean;
	    repo_root: string;
	
	    static createFrom(source: any = {}) {
	        return new SetupStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.needs_setup = source["needs_setup"];
	        this.repo_root = source["repo_root"];
	    }
	}
	
	export class TaskDraft {
	    text: string;
	    next_step?: string;
	    scheduled_for_utc?: string;
	    execution_class?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.next_step = source["next_step"];
	        this.scheduled_for_utc = source["scheduled_for_utc"];
	        this.execution_class = source["execution_class"];
	    }
	}
	export class TodoActionResponse {
	    applied: boolean;
	    item_id?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new TodoActionResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.applied = source["applied"];
	        this.item_id = source["item_id"];
	        this.error = source["error"];
	    }
	}
	
	export class WorkingNoteResponse {
	    path: string;
	    title: string;
	    content: string;
	    updated_at_utc?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkingNoteResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.updated_at_utc = source["updated_at_utc"];
	        this.error = source["error"];
	    }
	}
	export class WorkingNoteSaveRequest {
	    path?: string;
	    title?: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkingNoteSaveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.title = source["title"];
	        this.content = source["content"];
	    }
	}
	export class WorkingNoteSaveResponse {
	    saved: boolean;
	    path?: string;
	    title?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkingNoteSaveResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saved = source["saved"];
	        this.path = source["path"];
	        this.title = source["title"];
	        this.error = source["error"];
	    }
	}
	export class WorkingNoteSummary {
	    path: string;
	    title: string;
	    preview: string;
	    updated_at_utc: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkingNoteSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.title = source["title"];
	        this.preview = source["preview"];
	        this.updated_at_utc = source["updated_at_utc"];
	    }
	}
	export class WorkspaceCreateDirResponse {
	    created: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceCreateDirResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.created = source["created"];
	        this.error = source["error"];
	    }
	}
	export class WorkspaceDeleteResponse {
	    deleted: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceDeleteResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deleted = source["deleted"];
	        this.error = source["error"];
	    }
	}
	export class WorkspaceExportResponse {
	    exported: boolean;
	    host_path?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceExportResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exported = source["exported"];
	        this.host_path = source["host_path"];
	        this.error = source["error"];
	    }
	}
	export class WorkspaceImportResponse {
	    imported: boolean;
	    name?: string;
	    path?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceImportResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.imported = source["imported"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.error = source["error"];
	    }
	}
	export class WorkspaceListEntry {
	    name: string;
	    entry_type: string;
	    size_bytes: number;
	    mod_time_utc: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceListEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.entry_type = source["entry_type"];
	        this.size_bytes = source["size_bytes"];
	        this.mod_time_utc = source["mod_time_utc"];
	    }
	}
	export class WorkspaceListResponse {
	    path: string;
	    entries: WorkspaceListEntry[];
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceListResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.entries = this.convertValues(source["entries"], WorkspaceListEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkspacePreviewResponse {
	    content: string;
	    truncated: boolean;
	    path: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspacePreviewResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.truncated = source["truncated"];
	        this.path = source["path"];
	        this.error = source["error"];
	    }
	}
	export class WorkspaceRenameResponse {
	    renamed: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceRenameResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.renamed = source["renamed"];
	        this.error = source["error"];
	    }
	}
	export class WorkspaceRestoreResponse {
	    restored: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceRestoreResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.restored = source["restored"];
	        this.error = source["error"];
	    }
	}
	export class WorkspaceWriteResponse {
	    written: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceWriteResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.written = source["written"];
	        this.error = source["error"];
	    }
	}

}

export namespace threadstore {
	
	export class ConversationEvent {
	    v: string;
	    ts: string;
	    thread_id: string;
	    type: string;
	    data?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new ConversationEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.v = source["v"];
	        this.ts = source["ts"];
	        this.thread_id = source["thread_id"];
	        this.type = source["type"];
	        this.data = source["data"];
	    }
	}
	export class ThreadSummary {
	    thread_id: string;
	    title: string;
	    folder?: string;
	    workspace_id?: string;
	    created_at: string;
	    updated_at: string;
	    event_count: number;
	
	    static createFrom(source: any = {}) {
	        return new ThreadSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.thread_id = source["thread_id"];
	        this.title = source["title"];
	        this.folder = source["folder"];
	        this.workspace_id = source["workspace_id"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.event_count = source["event_count"];
	    }
	}

}


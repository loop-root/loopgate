import { useState } from "react";

import type { SecurityOverviewResponse } from "../../../wailsjs/go/main/HavenApp";
import type { SecurityAlert } from "../../lib/haven";
import { formatTime } from "../../lib/haven";

export interface LoopgateWindowProps {
  securityAlerts: SecurityAlert[];
  securityData: SecurityOverviewResponse | null;
  onClearSecurityAlerts: () => void;
  onUpdateTaskStandingGrant: (className: string, granted: boolean) => Promise<string | null>;
}

type CapabilityViewModel = {
  label: string;
  detail: string;
  bucket: "built_in" | "external";
};

type GroupedCapabilityView = CapabilityViewModel & {
  key: string;
  granted: boolean;
};

const capabilityLabels: Record<string, CapabilityViewModel> = {
  "fs_list": {
    label: "Browse Files",
    detail: "Look through folders and workspace contents",
    bucket: "built_in",
  },
  "fs_read": {
    label: "Read Documents",
    detail: "Open and inspect files in Haven",
    bucket: "built_in",
  },
  "fs_write": {
    label: "Save Work",
    detail: "Create and update files in Haven",
    bucket: "built_in",
  },
  "journal.list": {
    label: "Journal",
    detail: "Read, revisit, and write private reflections",
    bucket: "built_in",
  },
  "journal.read": {
    label: "Journal",
    detail: "Read, revisit, and write private reflections",
    bucket: "built_in",
  },
  "journal.write": {
    label: "Journal",
    detail: "Read, revisit, and write private reflections",
    bucket: "built_in",
  },
  "notes.list": {
    label: "Notes",
    detail: "Keep private plans, scratch work, and research notes",
    bucket: "built_in",
  },
  "notes.read": {
    label: "Notes",
    detail: "Keep private plans, scratch work, and research notes",
    bucket: "built_in",
  },
  "notes.write": {
    label: "Notes",
    detail: "Keep private plans, scratch work, and research notes",
    bucket: "built_in",
  },
  "memory.remember": {
    label: "Remember Things",
    detail: "Store a short durable memory fact",
    bucket: "built_in",
  },
  "paint.list": {
    label: "Paint",
    detail: "Create paintings and browse the gallery",
    bucket: "built_in",
  },
  "paint.save": {
    label: "Paint",
    detail: "Create paintings and browse the gallery",
    bucket: "built_in",
  },
  "note.create": {
    label: "Sticky Notes",
    detail: "Leave notes on the desktop",
    bucket: "built_in",
  },
  "todo.add": {
    label: "Task Board",
    detail: "Add, review, and complete operating tasks",
    bucket: "built_in",
  },
  "todo.complete": {
    label: "Task Board",
    detail: "Add, review, and complete operating tasks",
    bucket: "built_in",
  },
  "todo.list": {
    label: "Task Board",
    detail: "Add, review, and complete operating tasks",
    bucket: "built_in",
  },
  "shell_exec": {
    label: "Terminal Commands",
    detail: "Run commands when a task truly needs the terminal",
    bucket: "built_in",
  },
  "host.folder.list": {
    label: "List Host Folder",
    detail: "List files in a user-granted folder on the real Mac",
    bucket: "built_in",
  },
  "host.folder.read": {
    label: "Read Host File",
    detail: "Read a file from a granted host folder on disk",
    bucket: "built_in",
  },
  "host.organize.plan": {
    label: "Plan Host Organization",
    detail: "Draft a move/mkdir plan for a granted folder (no changes until apply)",
    bucket: "built_in",
  },
  "host.plan.apply": {
    label: "Apply Host Plan",
    detail: "Execute an approved organization plan on the real filesystem",
    bucket: "built_in",
  },
};

function describeCapability(name: string, category: string): CapabilityViewModel {
  const knownCapability = capabilityLabels[name];
  if (knownCapability) {
    return knownCapability;
  }
  if (category === "http") {
    return {
      label: prettifyCapabilityName(name),
      detail: "External connection capability",
      bucket: "external",
    };
  }
  return {
    label: prettifyCapabilityName(name),
    detail: "Registered system capability",
    bucket: "built_in",
  };
}

function prettifyCapabilityName(name: string): string {
  return name
    .replace(/[._]+/g, " ")
    .replace(/\b\w/g, (match) => match.toUpperCase());
}

export default function LoopgateWindow({ securityAlerts, securityData, onClearSecurityAlerts, onUpdateTaskStandingGrant }: LoopgateWindowProps) {
  const [busyGrantClass, setBusyGrantClass] = useState<string | null>(null);
  const capabilities = (securityData?.capabilities ?? []).map((capability) => ({
    capability,
    view: describeCapability(capability.name, capability.category),
  }));
  const groupedCapabilities = Object.values(capabilities.reduce<Record<string, GroupedCapabilityView>>((groups, { capability, view }) => {
    const groupKey = `${view.bucket}:${view.label}`;
    const existingGroup = groups[groupKey];
    if (existingGroup) {
      existingGroup.granted = existingGroup.granted || capability.granted;
      return groups;
    }
    groups[groupKey] = {
      key: groupKey,
      label: view.label,
      detail: view.detail,
      bucket: view.bucket,
      granted: capability.granted,
    };
    return groups;
  }, {}));
  const builtInCapabilities = groupedCapabilities.filter((capability) => capability.bucket === "built_in");
  const externalCapabilities = groupedCapabilities.filter((capability) => capability.bucket === "external");
  const connectionCount = securityData?.connections?.length ?? 0;
  const pendingApprovalCount = securityData?.pending_approvals?.length ?? 0;
  const activeMorphlingCount = securityData?.active_morphlings ?? 0;
  const standingTaskGrants = securityData?.standing_task_grants ?? [];
  const grantedStandingTaskCount = standingTaskGrants.filter((grant) => grant.granted).length;

  return (
    <div className="loopgate-app">
      <div className="loopgate-hero">
        <div className="loopgate-hero-kicker">Loopgate</div>
        <div className="loopgate-hero-title">Security and permissions for Morph's world.</div>
        <div className="loopgate-hero-copy">
          Haven-native abilities stay calm and low-friction here. Boundary crossings, approvals, and external connections stay visible.
        </div>
        <div className="loopgate-hero-pills">
          <span className="loopgate-hero-pill">{builtInCapabilities.length} built-in abilities</span>
          <span className="loopgate-hero-pill">{grantedStandingTaskCount} always allowed</span>
          <span className="loopgate-hero-pill status-pending">{pendingApprovalCount} approvals</span>
          <span className="loopgate-hero-pill">{connectionCount} connections</span>
          <span className="loopgate-hero-pill">{activeMorphlingCount} helpers</span>
        </div>
      </div>

      <div className="loopgate-section">
        <div className="loopgate-section-title">Always Allowed in Haven</div>
        {standingTaskGrants.length > 0 ? (
          <div className="lg-grant-list">
            {standingTaskGrants.map((grant) => (
              <div key={grant.class} className="lg-grant-card">
                <div className="lg-grant-main">
                  <div className="lg-grant-label-row">
                    <span className="lg-capability-label">{grant.label}</span>
                    <span className={`entry-result ${grant.granted ? "allowed" : "denied"}`}>
                      {grant.granted ? "always allowed" : "asks first"}
                    </span>
                  </div>
                  <div className="lg-capability-detail">{grant.description}</div>
                </div>
                <button
                  className={`lg-grant-toggle ${grant.granted ? "revoke" : "allow"}`}
                  onClick={() => {
                    setBusyGrantClass(grant.class);
                    void onUpdateTaskStandingGrant(grant.class, !grant.granted).finally(() => setBusyGrantClass(null));
                  }}
                  disabled={busyGrantClass === grant.class}
                >
                  {busyGrantClass === grant.class ? "Saving..." : grant.granted ? "Revoke" : "Allow"}
                </button>
              </div>
            ))}
          </div>
        ) : (
          <div className="loopgate-empty">No standing Haven task grants are configured yet.</div>
        )}
      </div>

      {securityAlerts.length > 0 && (
        <div className="loopgate-section lg-alerts-section">
          <div className="loopgate-section-title">
            Security Alerts
            <button className="lg-clear-btn" onClick={onClearSecurityAlerts}>Clear</button>
          </div>
          {securityAlerts.map((alert) => (
            <div key={alert.id} className="lg-alert-card">
              <span className="dot dot-red dot-pulse" />
              <div className="lg-alert-body">
                <div className="lg-alert-type">{alert.type === "loopgate_denial" ? "Request Denied" : alert.type}</div>
                <div className="lg-alert-msg">{alert.message}</div>
                <div className="lg-alert-time">{formatTime(alert.ts)}</div>
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="loopgate-section">
        <div className="loopgate-section-title">Pending Approvals</div>
        {securityData?.pending_approvals && securityData.pending_approvals.length > 0 ? (
          securityData.pending_approvals.map((approval) => (
            <div key={approval.approval_request_id} className="lg-approval-card">
              <div className="lg-approval-badge">
                <span className="dot dot-amber dot-pulse" />
                Approval Required
              </div>
              <div className="lg-approval-desc">
                <strong>{describeCapability(approval.capability, "tool").label}</strong>
                <span className="lg-approval-desc-detail">{describeCapability(approval.capability, "tool").detail}</span>
              </div>
              <div className="lg-approval-meta">
                <span>Capability ID: {approval.capability}</span>
                <span>Expires: {formatTime(approval.expires_at)}</span>
                {approval.redacted && <span>[REDACTED DETAILS]</span>}
              </div>
            </div>
          ))
        ) : (
          <div className="loopgate-empty">No pending approvals</div>
        )}
      </div>

      <div className="loopgate-section">
        <div className="loopgate-section-title">Granted Access</div>
        {builtInCapabilities.length > 0 && (
          <>
            <div className="lg-subsection-title">Built-In Abilities</div>
            {builtInCapabilities.map((capability) => (
              <div key={capability.key} className="lg-activity-entry">
                <span className={`dot ${capability.granted ? "dot-green" : "dot-red"}`} />
                <span className="entry-path">
                  <span className="lg-capability-label">{capability.label}</span>
                  <span className="lg-capability-detail">{capability.detail}</span>
                </span>
                <span className={`entry-result ${capability.granted ? "allowed" : "denied"}`}>
                  {capability.granted ? "granted" : "denied"}
                </span>
              </div>
            ))}
          </>
        )}
        {externalCapabilities.length > 0 && (
          <>
            <div className="lg-subsection-title">External Connections</div>
            {externalCapabilities.map((capability) => (
              <div key={capability.key} className="lg-activity-entry">
                <span className={`dot ${capability.granted ? "dot-green" : "dot-red"}`} />
                <span className="entry-path">
                  <span className="lg-capability-label">{capability.label}</span>
                  <span className="lg-capability-detail">{capability.detail}</span>
                </span>
                <span className={`entry-result ${capability.granted ? "allowed" : "denied"}`}>
                  {capability.granted ? "granted" : "denied"}
                </span>
              </div>
            ))}
          </>
        )}
        {capabilities.length === 0 && (
          <div className="loopgate-empty">No capabilities registered</div>
        )}
      </div>

      {connectionCount > 0 && (
        <div className="loopgate-section">
          <div className="loopgate-section-title">Connected Services</div>
          <div className="lg-connection-list">
            {(securityData?.connections ?? []).map((connection) => (
              <div key={`${connection.provider}-${connection.status}`} className="lg-connection-card">
                <div className="lg-connection-provider">{connection.provider}</div>
                <div className="lg-connection-status">{connection.status}</div>
                {connection.last_validated && (
                  <div className="lg-connection-time">Validated {formatTime(connection.last_validated)}</div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="loopgate-section">
        <div className="loopgate-section-title">Policy</div>
        {securityData?.policy && (
          <>
            <div className="lg-policy-row">
              <span className="policy-label">Read enabled</span>
              <span className={securityData.policy.read_enabled ? "on" : "off"}>
                {securityData.policy.read_enabled ? "YES" : "NO"}
              </span>
            </div>
            <div className="lg-policy-row">
              <span className="policy-label">Write enabled</span>
              <span className={securityData.policy.write_enabled ? "on" : "off"}>
                {securityData.policy.write_enabled ? "YES" : "NO"}
              </span>
            </div>
            <div className="lg-policy-row">
              <span className="policy-label">Write requires approval</span>
              <span className={securityData.policy.write_requires_approval ? "on" : "off"}>
                {securityData.policy.write_requires_approval ? "YES" : "NO"}
              </span>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

#!/usr/bin/env python3

import http.client
import json
import os
import pathlib
import socket
import sys
from typing import Any, Dict, Optional


HOOK_ENDPOINT = "/v1/hook/pre-validate"
DEFAULT_HOOK_TIMEOUT_SECONDS = 5.0
ALLOWED_REQUEST_FIELDS = {
    "hook_event_name",
    "tool_name",
    "tool_use_id",
    "tool_input",
    "prompt",
    "reason",
    "error",
    "is_interrupt",
    "cwd",
    "session_id",
}


class HookFailure(Exception):
    pass


class UnixSocketHTTPConnection(http.client.HTTPConnection):
    def __init__(self, socket_path: str, timeout_seconds: float) -> None:
        super().__init__("localhost", timeout=timeout_seconds)
        self.socket_path = socket_path

    def connect(self) -> None:
        connection_socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        connection_socket.settimeout(self.timeout)
        connection_socket.connect(self.socket_path)
        self.sock = connection_socket


def load_claude_hook_input(expected_event_name: str) -> Dict[str, Any]:
    try:
        raw_input = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        raise HookFailure(f"invalid Claude hook JSON input: {exc}") from exc

    if not isinstance(raw_input, dict):
        raise HookFailure("Claude hook input must be a JSON object")

    filtered_input = {}
    for field_name in ALLOWED_REQUEST_FIELDS:
        if field_name in raw_input:
            filtered_input[field_name] = raw_input[field_name]
    if not filtered_input.get("hook_event_name"):
        filtered_input["hook_event_name"] = expected_event_name
    return filtered_input


def resolve_loopgate_socket_path(request_payload: Dict[str, Any]) -> str:
    configured_socket_path = os.getenv("LOOPGATE_SOCKET", "").strip()
    if configured_socket_path:
        return configured_socket_path

    project_dir = os.getenv("CLAUDE_PROJECT_DIR", "").strip()
    if project_dir:
        return os.path.join(project_dir, "runtime", "state", "loopgate.sock")

    # Convenience-only local-dev fallback. For stable operator behavior, prefer the
    # operator-controlled LOOPGATE_SOCKET or CLAUDE_PROJECT_DIR values above.
    raw_cwd = request_payload.get("cwd")
    if isinstance(raw_cwd, str) and raw_cwd.strip():
        try:
            current_path = pathlib.Path(raw_cwd).expanduser().resolve()
        except OSError:
            current_path = None
        while current_path is not None:
            candidate_socket = current_path / "runtime" / "state" / "loopgate.sock"
            if candidate_socket.exists():
                return str(candidate_socket)
            parent_path = current_path.parent
            if parent_path == current_path:
                break
            current_path = parent_path

    raise HookFailure(
        "Loopgate socket path is not configured. Set LOOPGATE_SOCKET or run Claude Code from the Loopgate repo so CLAUDE_PROJECT_DIR points at it."
    )


def request_loopgate_hook_decision(request_payload: Dict[str, Any]) -> Dict[str, Any]:
    socket_path = resolve_loopgate_socket_path(request_payload)
    request_body = json.dumps(request_payload, separators=(",", ":")).encode("utf-8")
    connection = UnixSocketHTTPConnection(socket_path, DEFAULT_HOOK_TIMEOUT_SECONDS)
    try:
        connection.request(
            "POST",
            HOOK_ENDPOINT,
            body=request_body,
            headers={
                "Content-Type": "application/json",
                "Content-Length": str(len(request_body)),
            },
        )
        response = connection.getresponse()
        response_body = response.read()
    except (OSError, http.client.HTTPException) as exc:
        raise HookFailure(f"failed to contact Loopgate over {socket_path}: {exc}") from exc
    finally:
        connection.close()

    decoded_body = response_body.decode("utf-8", errors="replace").strip()
    if response.status != http.client.OK:
        detail = decoded_body or response.reason
        raise HookFailure(f"Loopgate hook validation failed with HTTP {response.status}: {detail}")

    try:
        parsed_response = json.loads(decoded_body or "{}")
    except json.JSONDecodeError as exc:
        raise HookFailure(f"Loopgate hook validation returned invalid JSON: {exc}") from exc

    if not isinstance(parsed_response, dict):
        raise HookFailure("Loopgate hook validation returned a non-object JSON payload")

    decision = parsed_response.get("decision")
    if decision not in {"allow", "ask", "block"}:
        raise HookFailure(f"Loopgate hook validation returned unsupported decision {decision!r}")
    return parsed_response


def additional_context_output(event_name: str, additional_context: str) -> Optional[Dict[str, Any]]:
    if not additional_context:
        return None
    return {
        "suppressOutput": True,
        "hookSpecificOutput": {
            "hookEventName": event_name,
            "additionalContext": additional_context,
        },
    }


def build_hook_output(event_name: str, response_payload: Dict[str, Any]) -> Optional[Dict[str, Any]]:
    decision = response_payload.get("decision", "")
    reason = response_payload.get("reason", "")
    additional_context = response_payload.get("additional_context", "")

    if event_name == "PreToolUse":
        permission_decision = {"allow": "allow", "ask": "ask", "block": "deny"}[decision]
        output = {
            "suppressOutput": True,
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": permission_decision,
            },
        }
        if reason:
            output["hookSpecificOutput"]["permissionDecisionReason"] = reason
        if additional_context:
            output["hookSpecificOutput"]["additionalContext"] = additional_context
        return output

    if event_name == "PermissionRequest":
        if decision == "ask":
            raise HookFailure("Loopgate returned unexpected ask decision for PermissionRequest")
        permission_request_output = {
            "suppressOutput": True,
            "hookSpecificOutput": {
                "hookEventName": "PermissionRequest",
                "decision": {
                    "behavior": "allow" if decision == "allow" else "deny",
                },
            },
        }
        if decision == "block" and reason:
            permission_request_output["hookSpecificOutput"]["decision"]["message"] = reason
        return permission_request_output

    if event_name == "SessionStart":
        if decision == "ask":
            raise HookFailure("Loopgate returned unexpected ask decision for SessionStart")
        if decision == "block":
            raise HookFailure(reason or "Loopgate blocked SessionStart")
        return additional_context_output("SessionStart", additional_context)

    if event_name == "SessionEnd":
        if decision == "ask":
            raise HookFailure("Loopgate returned unexpected ask decision for SessionEnd")
        if decision == "block":
            raise HookFailure(reason or "Loopgate blocked SessionEnd")
        return None

    if event_name == "UserPromptSubmit":
        if decision == "ask":
            raise HookFailure("Loopgate returned unexpected ask decision for UserPromptSubmit")
        output = additional_context_output("UserPromptSubmit", additional_context)
        has_output_content = output is not None
        if output is None:
            output = {"suppressOutput": True}
        if decision == "block":
            output["decision"] = "block"
            output["reason"] = reason or "Loopgate blocked prompt processing"
            has_output_content = True
        return output if has_output_content else None

    if event_name == "PostToolUse":
        if decision == "ask":
            raise HookFailure("Loopgate returned unexpected ask decision for PostToolUse")
        output = additional_context_output("PostToolUse", additional_context)
        has_output_content = output is not None
        if output is None:
            output = {"suppressOutput": True}
        if decision == "block":
            output["decision"] = "block"
            output["reason"] = reason or "Loopgate rejected post-tool processing"
            has_output_content = True
        return output if has_output_content else None

    if event_name == "PostToolUseFailure":
        if decision == "ask":
            raise HookFailure("Loopgate returned unexpected ask decision for PostToolUseFailure")
        output = additional_context_output("PostToolUseFailure", additional_context)
        has_output_content = output is not None
        if output is None:
            output = {"suppressOutput": True}
        if decision == "block":
            output["decision"] = "block"
            output["reason"] = reason or "Loopgate rejected post-tool failure processing"
            has_output_content = True
        return output if has_output_content else None

    raise HookFailure(f"unsupported Loopgate hook event {event_name!r}")


def main(expected_event_name: str) -> int:
    try:
        request_payload = load_claude_hook_input(expected_event_name)
        response_payload = request_loopgate_hook_decision(request_payload)
        hook_output = build_hook_output(expected_event_name, response_payload)
    except HookFailure as exc:
        # Intentional fail-closed path. Claude Code's documented hook semantics treat
        # exit code 2 as blocking for PreToolUse, PermissionRequest, and
        # UserPromptSubmit, while non-blockable lifecycle hooks surface the error to
        # the user or Claude instead of silently bypassing Loopgate.
        print(f"Loopgate hook error: {exc}", file=sys.stderr)
        return 2

    if hook_output is not None:
        print(json.dumps(hook_output, separators=(",", ":")))
    return 0

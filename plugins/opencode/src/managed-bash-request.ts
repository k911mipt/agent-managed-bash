import type { ToolContext } from "@opencode-ai/plugin"
import type { ManagedBashAction } from "./action-schema"
import type { Request, TrustedContext } from "./generated/protocol.gen"

export function trustedContextFor(context: ToolContext): TrustedContext {
  return {
    session_id: context.sessionID,
    workspace_path: context.worktree === "/" ? context.directory : context.worktree,
    cwd: context.directory,
  }
}

export function requestFor(action: ManagedBashAction, context: TrustedContext): Request {
  switch (action.action) {
    case "run":
      return {
        schema_version: 1,
        action: "run",
        context,
        payload: {
          command: action.command,
          ...(action.hard_timeout_ms === undefined ? {} : { hard_timeout_ms: action.hard_timeout_ms }),
          ...(action.output_limit_bytes === undefined ? {} : { output_limit_bytes: action.output_limit_bytes }),
        },
      }
    case "wait":
      return {
        schema_version: 1,
        action: "wait",
        context,
        payload: {
          job_id: action.job_id,
          ...(action.cursor_bytes === undefined ? {} : { cursor_bytes: action.cursor_bytes }),
          ...(action.timeout_ms === undefined ? {} : { timeout_ms: action.timeout_ms }),
          ...(action.idle_timeout_ms === undefined ? {} : { idle_timeout_ms: action.idle_timeout_ms }),
        },
      }
    case "status":
    case "cancel":
    case "remove":
      return { schema_version: 1, action: action.action, context, payload: { job_id: action.job_id } }
    case "output":
      return {
        schema_version: 1,
        action: "output",
        context,
        payload: {
          job_id: action.job_id,
          ...(action.start_cursor_bytes === undefined ? {} : { start_cursor_bytes: action.start_cursor_bytes }),
          ...(action.end_cursor_bytes === undefined ? {} : { end_cursor_bytes: action.end_cursor_bytes }),
        },
      }
    case "list":
      return { schema_version: 1, action: "list", context }
    case "version":
      return { schema_version: 1, action: "version" }
    default:
      return assertNever(action)
  }
}

function assertNever(value: never): never {
  throw new TypeError(`unreachable action: ${String(value)}`)
}

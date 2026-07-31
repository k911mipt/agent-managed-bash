import type { ToolContext } from "@opencode-ai/plugin"
import type { CliExecutor } from "./cli-transport"
import type { Request, Response } from "./generated/protocol.gen"

const encoder = new TextEncoder()

export function toolContext(sessionID: string): ToolContext {
  return {
    sessionID,
    messageID: `message-${sessionID}`,
    agent: "build",
    directory: "/workspace/project",
    worktree: "/workspace",
    abort: new AbortController().signal,
    metadata: () => undefined,
    ask: async () => undefined,
  }
}

export function lifecycleExecutor(calls: Request[]): CliExecutor {
  return {
    async execute(request) {
      calls.push(request)
      switch (request.action) {
        case "version":
          return response(versionResponse())
        case "start":
          return response(startResponse(request.context.session_id, request.payload.command))
        case "run":
          return response(runResponse(request.context.session_id, request.payload.command))
        case "cancel":
          return response(cancellationResponse(request))
        default:
          throw new TypeError(`unexpected action: ${request.action}`)
      }
    },
  }
}

export function cancellationResponse(request: Extract<Request, { action: "cancel" }>): Extract<Response, { action: "cancel" }> {
  return {
    schema_version: 1,
    ok: true,
    action: "cancel",
    result: {
      job_id: request.payload.job_id,
      status: "running",
      outcome: "requested",
      cancellation: {
        requested: true,
        requested_at_unix_ms: 1,
        requested_by_session_id: request.context.session_id,
      },
    },
  }
}

export function response(value: Response): { readonly exitCode: number; readonly stderr: Uint8Array; readonly stdout: Uint8Array } {
  return {
    exitCode: value.ok ? 0 : 5,
    stderr: new Uint8Array(),
    stdout: encoder.encode(`${JSON.stringify(value)}\n`),
  }
}

export function versionResponse(): Extract<Response, { action: "version" }> {
  return {
    schema_version: 1,
    ok: true,
    action: "version",
    result: {
      product: "managed-bash",
      binary_version: "dev",
      protocol_version: 1,
      os: "linux",
      architecture: "amd64",
    },
  }
}

export function runResponse(sessionID: string, command: string): Extract<Response, { action: "run" }> {
  return {
    schema_version: 1,
    ok: true,
    action: "run",
    result: {
      reason: "output_idle",
      observation: { job: jobMetadata(sessionID, command) },
      output: {
        text: "",
        start_cursor_bytes: 0,
        next_cursor_bytes: 0,
        captured_bytes: 0,
        eof: false,
      },
    },
  }
}

export function recoverableRunError(sessionID: string, command: string): Extract<Response, { ok: false }> {
  return {
    schema_version: 1,
    ok: false,
    action: "run",
    error: { code: "io_failure", message: "I/O operation failed" },
    job: jobMetadata(sessionID, command),
  }
}

export function startResponse(sessionID: string, command: string): Extract<Response, { action: "start" }> {
  return { schema_version: 1, ok: true, action: "start", result: jobMetadata(sessionID, command) }
}

function jobMetadata(
  sessionID: string,
  command: string,
): Extract<Response, { action: "start" }>["result"] & { readonly status: "running" } {
  return {
    job_id: `job-${sessionID}`,
    status: "running",
    owner_session_id: sessionID,
    workspace_path: "/workspace",
    cwd: "/workspace/project",
    command,
    created_at_unix_ms: 1,
    started_at_unix_ms: 1,
    captured_bytes: 0,
    hard_timeout_ms: 7_200_000,
    output_limit_bytes: 104_857_600,
  }
}

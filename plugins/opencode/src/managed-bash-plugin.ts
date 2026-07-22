import { resolve } from "node:path"
import {
  tool,
  type Plugin,
  type ToolContext,
  type ToolDefinition,
  type ToolResult,
} from "@opencode-ai/plugin"
import { managedBashActionSchema, managedBashToolArgs, type ManagedBashAction } from "./action-schema"
import {
  CliProtocolError,
  createBunCliExecutor,
  executeProtocolRequest,
  type CliExecutor,
} from "./cli-transport"
import type { Request, Response, TrustedContext } from "./generated/protocol.gen"
import { formatProtocolResponse, formatToolError } from "./presentation"
import { createResponseValidator } from "./response-validator"

export type ManagedBashController = {
  execute(input: unknown, context: ToolContext): Promise<ToolResult>
  handleEvent(event: unknown): Promise<void>
  dispose(): Promise<void>
}

export type ManagedBashControllerOptions = {
  readonly executor?: CliExecutor
  readonly repositoryRoot?: string
}

type TrackedJob = {
  readonly jobID: string
  readonly context: TrustedContext
}

type SessionState = {
  readonly jobs: Map<string, TrackedJob>
  readonly pendingRuns: Set<Promise<void>>
  closing: boolean
}

type PendingRun = {
  readonly state: SessionState
  finish(): void
}

export async function createManagedBashController(
  options: ManagedBashControllerOptions = {},
): Promise<ManagedBashController> {
  const repositoryRoot = options.repositoryRoot ?? resolve(import.meta.dir, "../../..")
  const validateResponse = await createResponseValidator(repositoryRoot)
  const executor = options.executor ?? createBunCliExecutor()
  const closedSessionIDs = new Set<string>()
  const sessions = new Map<string, SessionState>()
  let disposed = false
  let versionHandshake: Promise<void> | undefined

  async function execute(input: unknown, context: ToolContext): Promise<ToolResult> {
    const parsed = managedBashActionSchema.safeParse(input)
    if (!parsed.success) {
      return formatToolError("invalid_request", parsed.error.message)
    }

    const action = parsed.data
    context.metadata({
      title: `managed_bash ${action.action}`,
      metadata: { call_id: context.messageID, session_id: context.sessionID },
    })
    if (action.action === "run") {
      return executeRun(action, context)
    }

    try {
      if (action.action !== "version") {
        await ensureVersion()
      }
      const response = await executeProtocolRequest(
        executor,
        requestFor(action, trustedContextFor(context)),
        context.abort,
        validateResponse,
      )
      return formatProtocolResponse(response)
    } catch (error: unknown) {
      return formatExecutionError(error)
    }
  }

  async function executeRun(
    action: Extract<ManagedBashAction, { action: "run" }>,
    context: ToolContext,
  ): Promise<ToolResult> {
    await context.ask({
      permission: "bash",
      patterns: [action.command],
      always: [action.command],
      metadata: { command: action.command },
    })
    if (context.abort.aborted) {
      return formatToolError("runner_aborted", "run was aborted before launch")
    }

    const pendingRun = beginRun(context.sessionID)
    if (pendingRun === undefined) {
      return formatToolError("runner_aborted", "session is closing")
    }

    const trustedContext = trustedContextFor(context)
    try {
      await ensureVersion()
      const response = await executeProtocolRequest(
        executor,
        requestFor(action, trustedContext),
        new AbortController().signal,
        validateResponse,
      )
      trackRun(response, trustedContext, pendingRun.state)
      if (context.abort.aborted && response.ok && response.action === "run") {
        await cancelTrackedJob({ jobID: response.result.job_id, context: trustedContext })
      }
      return formatProtocolResponse(response)
    } catch (error: unknown) {
      return formatExecutionError(error)
    } finally {
      pendingRun.finish()
    }
  }

  async function ensureVersion(): Promise<void> {
    if (versionHandshake !== undefined) {
      return versionHandshake
    }

    const pending = executeProtocolRequest(
      executor,
      { schema_version: 1, action: "version" },
      new AbortController().signal,
      validateResponse,
    ).then((response) => {
      if (!response.ok) {
        throw new CliProtocolError(`version handshake failed with ${response.error.code}`)
      }
      if (
        response.action !== "version" ||
        response.result.product !== "managed-bash" ||
        response.result.protocol_version !== 1
      ) {
        throw new CliProtocolError("version handshake reported an incompatible managed-bash binary")
      }
    })
    versionHandshake = pending
    try {
      await pending
    } catch (error: unknown) {
      if (versionHandshake === pending) {
        versionHandshake = undefined
      }
      throw error
    }
  }

  function trackRun(response: Response, context: TrustedContext, state: SessionState): void {
    if (!response.ok || response.action !== "run") {
      return
    }

    state.jobs.set(response.result.job_id, { jobID: response.result.job_id, context })
  }

  async function cancelSession(sessionID: string): Promise<void> {
    closedSessionIDs.add(sessionID)
    const state = sessions.get(sessionID)
    if (state === undefined) {
      return
    }
    state.closing = true
    await Promise.allSettled([...state.pendingRuns])
    await Promise.allSettled([...state.jobs.values()].map((job) => cancelTrackedJob(job)))
    sessions.delete(sessionID)
  }

  async function cancelTrackedJob(job: TrackedJob): Promise<void> {
    try {
      const signal = new AbortController().signal
      await ensureVersion()
      await executeProtocolRequest(
        executor,
        {
          schema_version: 1,
          action: "cancel",
          context: job.context,
          payload: { job_id: job.jobID },
        },
        signal,
        validateResponse,
      )
    } catch {
      return
    }
  }

  function beginRun(sessionID: string): PendingRun | undefined {
    if (disposed || closedSessionIDs.has(sessionID)) {
      return undefined
    }
    const state = sessions.get(sessionID) ?? {
      jobs: new Map<string, TrackedJob>(),
      pendingRuns: new Set<Promise<void>>(),
      closing: false,
    }
    sessions.set(sessionID, state)
    if (state.closing) {
      return undefined
    }

    let resolve: (() => void) | undefined
    const completion = new Promise<void>((complete) => {
      resolve = complete
    })
    state.pendingRuns.add(completion)
    return {
      state,
      finish() {
        state.pendingRuns.delete(completion)
        resolve?.()
      },
    }
  }

  return {
    execute,
    async handleEvent(event) {
      const sessionID = deletedSessionID(event)
      if (sessionID !== undefined) {
        await cancelSession(sessionID)
      }
    },
    async dispose() {
      disposed = true
      await Promise.allSettled([...sessions.keys()].map((sessionID) => cancelSession(sessionID)))
    },
  }
}

export function createManagedBashTool(controller: ManagedBashController): ToolDefinition {
  return tool({
    description: "Run and observe cancellable managed shell jobs.",
    args: managedBashToolArgs,
    execute: (input, context) => controller.execute(input, context),
  })
}

export function createManagedBashPlugin(options: ManagedBashControllerOptions = {}): Plugin {
  return async () => {
    const controller = await createManagedBashController(options)
    return {
      tool: { managed_bash: createManagedBashTool(controller) },
      event: async ({ event }) => controller.handleEvent(event),
      dispose: () => controller.dispose(),
    }
  }
}

export const ManagedBashPlugin: Plugin = createManagedBashPlugin()

function trustedContextFor(context: ToolContext): TrustedContext {
  return {
    session_id: context.sessionID,
    workspace_path: context.worktree,
    cwd: context.directory,
  }
}

function requestFor(action: ManagedBashAction, context: TrustedContext): Request {
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

function deletedSessionID(event: unknown): string | undefined {
  if (!isRecord(event) || event["type"] !== "session.deleted" || !isRecord(event["properties"])) {
    return undefined
  }
  const info = event["properties"]["info"]
  return isRecord(info) && typeof info["id"] === "string" ? info["id"] : undefined
}

function formatExecutionError(error: unknown): ToolResult {
  if (error instanceof CliProtocolError) {
    return formatToolError("runner_protocol_error", error.message)
  }
  if (error instanceof DOMException && error.name === "AbortError") {
    return formatToolError("runner_aborted", error.message)
  }
  return formatToolError("runner_transport_error", error instanceof Error ? error.message : "unknown runner failure")
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function assertNever(value: never): never {
  throw new TypeError(`unreachable action: ${String(value)}`)
}

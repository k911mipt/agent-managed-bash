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
import type { Response, TrustedContext } from "./generated/protocol.gen"
import { formatExecutionError } from "./managed-bash-error"
import { deletedSessionID } from "./managed-bash-event"
import { requestFor, trustedContextFor } from "./managed-bash-request"
import { formatProtocolResponse, formatToolError } from "./presentation"
import { managedBashReleaseVersion } from "./release-version"
import { createResponseValidator } from "./response-validator"

export type ManagedBashController = {
  execute(input: unknown, context: ToolContext): Promise<ToolResult>
  handleEvent(event: unknown): Promise<void>
  dispose(): Promise<void>
}

export type ManagedBashControllerOptions = {
  readonly executor?: CliExecutor
  readonly pluginVersion?: string
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
  const validateResponse = createResponseValidator()
  const executor = options.executor ?? createBunCliExecutor()
  const pluginVersion = options.pluginVersion ?? managedBashReleaseVersion
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
    } catch (error: unknown) { // no-excuse-ok: catch — formatExecutionError owns exhaustive narrowing.
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
    } catch (error: unknown) { // no-excuse-ok: catch — formatExecutionError owns exhaustive narrowing.
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
        response.result.binary_version !== pluginVersion ||
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
    description: "Run and observe cancellable managed shell jobs. run starts jobs; wait owns observation timeouts.",
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

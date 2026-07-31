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
import { createSessionRegistry, type TrackedJob } from "./managed-bash-session-registry"
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

export async function createManagedBashController(
  options: ManagedBashControllerOptions = {},
): Promise<ManagedBashController> {
  const validateResponse = createResponseValidator()
  const executor = options.executor ?? createBunCliExecutor()
  const pluginVersion = options.pluginVersion ?? managedBashReleaseVersion
  const sessionRegistry = createSessionRegistry()
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
    if (action.action === "start" || action.action === "run") {
      return executeLaunch(action, context)
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

  async function executeLaunch(
    action: Extract<ManagedBashAction, { action: "start" | "run" }>,
    context: ToolContext,
  ): Promise<ToolResult> {
    await context.ask({
      permission: "bash",
      patterns: [action.command],
      always: [action.command],
      metadata: { command: action.command },
    })
    if (context.abort.aborted) {
      return formatToolError("runner_aborted", `${action.action} was aborted before launch`)
    }

    const pendingLaunch = sessionRegistry.begin(context.sessionID, action.action === "run")
    if (pendingLaunch === undefined) {
      return formatToolError("runner_aborted", "session is closing")
    }

    const trustedContext = trustedContextFor(context)
    try {
      await ensureVersion()
      const response = await executeProtocolRequest(
        executor,
        requestFor(action, trustedContext),
        AbortSignal.any([context.abort, pendingLaunch.signal]),
        validateResponse,
      )
      const tracked = trackedJobFromResponse(response, trustedContext)
      if (tracked !== undefined) {
        pendingLaunch.track(tracked)
      }
      if (context.abort.aborted && tracked !== undefined) {
        await cancelTrackedJob(tracked)
      }
      return formatProtocolResponse(response)
    } catch (error: unknown) { // no-excuse-ok: catch — formatExecutionError owns exhaustive narrowing.
      return formatExecutionError(error)
    } finally {
      pendingLaunch.finish()
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

  function trackedJobFromResponse(response: Response, context: TrustedContext): TrackedJob | undefined {
    const jobID = response.ok
      ? response.action === "start"
        ? response.result.job_id
        : response.action === "run"
          ? response.result.observation.job.job_id
          : undefined
      : response.action === "run"
        ? response.job?.job_id
        : undefined
    if (jobID === undefined) {
      return undefined
    }
    return { jobID, context }
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

  return {
    execute,
    async handleEvent(event) {
      const sessionID = deletedSessionID(event)
      if (sessionID !== undefined) {
        await sessionRegistry.close(sessionID, cancelTrackedJob)
      }
    },
    async dispose() {
      await sessionRegistry.dispose(cancelTrackedJob)
    },
  }
}

export function createManagedBashTool(controller: ManagedBashController): ToolDefinition {
  return tool({
    description: "Run and observe cancellable managed shell jobs. run observes until terminal or checkpoint; start detaches immediately.",
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

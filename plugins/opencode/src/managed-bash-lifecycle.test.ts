import { describe, expect, test } from "bun:test"
import { resolve } from "node:path"
import type { ToolContext } from "@opencode-ai/plugin"
import type { CliExecutor } from "./cli-transport"
import type { Request, Response } from "./generated/protocol.gen"
import { createManagedBashController } from "./managed-bash-plugin"

const encoder = new TextEncoder()
const repositoryRoot = resolve(import.meta.dir, "../../..")

describe("managed_bash lifecycle cleanup", () => {
  test("session.deleted cancels only jobs tracked for that session", async () => {
    const calls: Request[] = []
    const controller = await createManagedBashController({
      executor: lifecycleExecutor(calls),
      repositoryRoot,
    })

    await controller.execute({ action: "run", command: "sleep 1" }, toolContext("session-a"))
    await controller.execute({ action: "run", command: "sleep 1" }, toolContext("session-b"))
    await controller.handleEvent({
      type: "session.deleted",
      properties: { info: { id: "session-a" } },
    })

    expect(calls.filter((request) => request.action === "cancel")).toEqual([
      {
        schema_version: 1,
        action: "cancel",
        context: {
          session_id: "session-a",
          workspace_path: "/workspace",
          cwd: "/workspace/project",
        },
        payload: { job_id: "job-session-a" },
      },
    ])
  })

  test("dispose performs best-effort cancellation for every tracked owner session", async () => {
    const calls: Request[] = []
    const controller = await createManagedBashController({
      executor: lifecycleExecutor(calls),
      repositoryRoot,
    })

    await controller.execute({ action: "run", command: "sleep 1" }, toolContext("session-a"))
    await controller.execute({ action: "run", command: "sleep 1" }, toolContext("session-b"))
    await controller.dispose()

    expect(calls.filter((request) => request.action === "cancel")).toHaveLength(2)
  })

  test("session.deleted waits for an in-flight run before cancelling its committed job", async () => {
    const calls: Request[] = []
    let releaseRun: ((response: ReturnType<typeof runResponse>) => void) | undefined
    let notifyRunStarted: (() => void) | undefined
    const runStarted = new Promise<void>((resolve) => {
      notifyRunStarted = resolve
    })
    const executor: CliExecutor = {
      async execute(request) {
        calls.push(request)
        switch (request.action) {
          case "version":
            return response(versionResponse())
          case "run":
            notifyRunStarted?.()
            return await new Promise((resolve) => {
              releaseRun = (runResponse) => resolve(response(runResponse))
            })
          case "cancel":
            return response(cancellationResponse(request))
          default:
            throw new TypeError(`unexpected action: ${request.action}`)
        }
      },
    }
    const controller = await createManagedBashController({ executor, repositoryRoot })

    const run = controller.execute({ action: "run", command: "sleep 1" }, toolContext("session-a"))
    await runStarted
    const cleanup = controller.handleEvent({
      type: "session.deleted",
      properties: { info: { id: "session-a" } },
    })
    releaseRun?.(runResponse("session-a", "sleep 1"))

    await Promise.all([run, cleanup])

    expect(calls.filter((request) => request.action === "cancel")).toHaveLength(1)
  })

  test("session.deleted prevents a run from launching after delayed permission approval", async () => {
    const calls: Request[] = []
    let releasePermission: (() => void) | undefined
    let notifyPermissionRequested: (() => void) | undefined
    const permissionRequested = new Promise<void>((resolve) => {
      notifyPermissionRequested = resolve
    })
    const controller = await createManagedBashController({
      executor: lifecycleExecutor(calls),
      repositoryRoot,
    })
    const context = toolContext("session-a")
    context.ask = async () => {
      notifyPermissionRequested?.()
      await new Promise<void>((resolve) => {
        releasePermission = resolve
      })
    }

    const run = controller.execute({ action: "run", command: "sleep 1" }, context)
    await permissionRequested
    await controller.handleEvent({
      type: "session.deleted",
      properties: { info: { id: "session-a" } },
    })
    releasePermission?.()

    const result = await run

    expect(calls).toEqual([])
    expect(typeof result).toBe("object")
    if (typeof result === "string") {
      throw new TypeError("expected structured tool result")
    }
    expect(result.output).toContain("runner_aborted")
  })
})

function toolContext(sessionID: string): ToolContext {
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

function lifecycleExecutor(calls: Request[]): CliExecutor {
  return {
    async execute(request) {
      calls.push(request)
      switch (request.action) {
        case "version":
          return response(versionResponse())
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

function cancellationResponse(request: Extract<Request, { action: "cancel" }>): Extract<Response, { action: "cancel" }> {
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

function response(value: Response): { readonly exitCode: number; readonly stderr: Uint8Array; readonly stdout: Uint8Array } {
  return {
    exitCode: value.ok ? 0 : 5,
    stderr: new Uint8Array(),
    stdout: encoder.encode(`${JSON.stringify(value)}\n`),
  }
}

function versionResponse(): Extract<Response, { action: "version" }> {
  return {
    schema_version: 1,
    ok: true,
    action: "version",
    result: {
      product: "managed-bash",
      binary_version: "test",
      protocol_version: 1,
      os: "linux",
      architecture: "amd64",
    },
  }
}

function runResponse(sessionID: string, command: string): Extract<Response, { action: "run" }> {
  return {
    schema_version: 1,
    ok: true,
    action: "run",
    result: {
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
    },
  }
}

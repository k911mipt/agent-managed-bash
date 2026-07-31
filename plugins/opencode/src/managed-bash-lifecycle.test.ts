import { describe, expect, test } from "bun:test"
import type { CliExecutor } from "./cli-transport"
import type { Request } from "./generated/protocol.gen"
import {
  cancellationResponse,
  lifecycleExecutor,
  recoverableRunError,
  response,
  runResponse,
  startResponse,
  toolContext,
  versionResponse,
} from "./managed-bash-lifecycle-fixtures"
import { createManagedBashController } from "./managed-bash-plugin"

describe("managed_bash lifecycle cleanup", () => {
  test("session.deleted cancels only jobs tracked for that session", async () => {
    const calls: Request[] = []
    const controller = await createManagedBashController({
      executor: lifecycleExecutor(calls),
    })

    await controller.execute({ action: "start", command: "sleep 1" }, toolContext("session-a"))
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
    })

    await controller.execute({ action: "start", command: "sleep 1" }, toolContext("session-a"))
    await controller.execute({ action: "run", command: "sleep 1" }, toolContext("session-b"))
    await controller.dispose()

    expect(calls.filter((request) => request.action === "cancel")).toHaveLength(2)
  })

  test("session.deleted aborts an in-flight run before cancelling its committed job", async () => {
    const calls: Request[] = []
    let notifyRunStarted: (() => void) | undefined
    const runStarted = new Promise<void>((resolve) => {
      notifyRunStarted = resolve
    })
    const executor: CliExecutor = {
      async execute(request, signal) {
        calls.push(request)
        switch (request.action) {
          case "version":
            return response(versionResponse())
          case "run":
            notifyRunStarted?.()
            return await new Promise((resolve) => {
              signal.addEventListener("abort", () => {
                resolve(response(recoverableRunError(request.context.session_id, request.payload.command)))
              }, { once: true })
            })
          case "cancel":
            return response(cancellationResponse(request))
          default:
            throw new TypeError(`unexpected action: ${request.action}`)
        }
      },
    }
    const controller = await createManagedBashController({ executor })

    const run = controller.execute({ action: "run", command: "sleep 1" }, toolContext("session-a"))
    await runStarted
    const cleanup = controller.handleEvent({
      type: "session.deleted",
      properties: { info: { id: "session-a" } },
    })

    const [result] = await Promise.all([run, cleanup])

    expect(calls.filter((request) => request.action === "cancel")).toHaveLength(1)
    expect(typeof result).toBe("object")
    if (typeof result === "string") {
      throw new TypeError("expected structured tool result")
    }
    expect(result.output).toContain("job job-session-a remains available")
  })

  test("session.deleted returns before an in-flight start and cancels its job when launch completes", async () => {
    const calls: Request[] = []
    let releaseStart: ((response: ReturnType<typeof startResponse>) => void) | undefined
    let notifyStartStarted: (() => void) | undefined
    let notifyCancelled: (() => void) | undefined
    const startStarted = new Promise<void>((resolve) => {
      notifyStartStarted = resolve
    })
    const cancelled = new Promise<void>((resolve) => {
      notifyCancelled = resolve
    })
    const executor: CliExecutor = {
      async execute(request) {
        calls.push(request)
        switch (request.action) {
          case "version":
            return response(versionResponse())
          case "start":
            notifyStartStarted?.()
            return await new Promise((resolve) => {
              releaseStart = (start) => resolve(response(start))
            })
          case "cancel":
            notifyCancelled?.()
            return response(cancellationResponse(request))
          default:
            throw new TypeError(`unexpected action: ${request.action}`)
        }
      },
    }
    const controller = await createManagedBashController({ executor })

    const start = controller.execute({ action: "start", command: "sleep 1" }, toolContext("session-a"))
    await startStarted
    await controller.handleEvent({ type: "session.deleted", properties: { info: { id: "session-a" } } })
    releaseStart?.(startResponse("session-a", "sleep 1"))
    await Promise.all([start, cancelled])

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

  test("session.deleted cancels a job recovered from a run observation error", async () => {
    // Given
    const calls: Request[] = []
    const executor: CliExecutor = {
      async execute(request) {
        calls.push(request)
        switch (request.action) {
          case "version":
            return response(versionResponse())
          case "run":
            return response(recoverableRunError(request.context.session_id, request.payload.command))
          case "cancel":
            return response(cancellationResponse(request))
          default:
            throw new TypeError(`unexpected action: ${request.action}`)
        }
      },
    }
    const controller = await createManagedBashController({ executor })

    // When
    await controller.execute({ action: "run", command: "sleep 1" }, toolContext("session-a"))
    await controller.handleEvent({ type: "session.deleted", properties: { info: { id: "session-a" } } })

    // Then
    expect(calls.filter((request) => request.action === "cancel")).toHaveLength(1)
  })
})

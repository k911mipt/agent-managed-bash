import { describe, expect, test } from "bun:test"
import type { ToolContext } from "@opencode-ai/plugin"
import type { Request, Response } from "./generated/protocol.gen"
import type { CliExecutor } from "./cli-transport"
import { checkpointMetadata } from "./managed-bash-checkpoint"
import { createManagedBashController } from "./managed-bash-plugin"

const encoder = new TextEncoder()

describe("managed_bash", () => {
  test("asks native bash permission before the version handshake and run request", async () => {
    // Given
    const calls: Request[] = []
    const executor = fakeExecutor(calls)
    const controller = await createManagedBashController({ executor })
    const denied = new Error("permission denied")
    const asks: unknown[] = []
    const context = toolContext(async (request) => {
      asks.push(request)
      throw denied
    })

    // When / Then
    await expect(controller.execute({ action: "run", command: "printf ok" }, context)).rejects.toBe(denied)
    expect(asks).toEqual([
      {
        permission: "bash",
        patterns: ["printf ok"],
        always: ["printf ok"],
        metadata: { command: "printf ok" },
      },
    ])
    expect(calls).toEqual([])
  })

  test("derives trusted context from OpenCode and never from tool arguments", async () => {
    // Given
    const calls: Request[] = []
    const controller = await createManagedBashController({
      executor: fakeExecutor(calls),
    })
    const context = toolContext()

    // When
    await controller.execute({ action: "run", command: "printf ok" }, context)

    // Then
    const run = calls[1]
    expect(run).toEqual({
      schema_version: 1,
      action: "run",
      context: {
        session_id: "session-1",
        workspace_path: "/workspace",
        cwd: "/workspace/project",
      },
      payload: { command: "printf ok" },
    })
  })

  test("returns a structured protocol error instead of falling back after malformed CLI output", async () => {
    // Given
    const calls: Request[] = []
    const executor: CliExecutor = {
      async execute(request) {
        calls.push(request)
        if (request.action === "version") {
          return response(versionResponse())
        }
        return { exitCode: 5, stderr: new Uint8Array(), stdout: encoder.encode("not json\n") }
      },
    }
    const controller = await createManagedBashController({ executor })

    // When
    const result = await controller.execute({ action: "run", command: "printf ok" }, toolContext())

    // Then
    expect(calls).toHaveLength(2)
    expect(typeof result).toBe("object")
    if (typeof result === "string") {
      throw new TypeError("expected structured tool result")
    }
    expect(result.title).toBe("managed_bash error")
    expect(result.output).toContain("runner_protocol_error")
    expect(result.metadata).toEqual({ code: "runner_protocol_error" })
  })

  test("renders normal responses without repeating the quiet output limit", async () => {
    // Given
    const calls: Request[] = []
    const controller = await createManagedBashController({
      executor: fakeExecutor(calls),
    })

    // When
    const result = await controller.execute({ action: "run", command: "printf ok" }, toolContext())

    // Then
    expect(typeof result).toBe("object")
    if (typeof result === "string") {
      throw new TypeError("expected structured tool result")
    }
    expect(result.output).toContain("job-1")
    expect(result.output).not.toContain("104857600")
    expect(result.metadata).toBeUndefined()
  })

  test("does not repeat command permission for an existing job operation", async () => {
    const calls: Request[] = []
    const controller = await createManagedBashController({
      executor: fakeExecutor(calls),
    })
    let asks = 0

    const result = await controller.execute(
      { action: "wait", job_id: "job-1" },
      toolContext(async () => {
        asks += 1
      }),
    )

    expect(asks).toBe(0)
    expect(calls[1]).toEqual({
      schema_version: 1,
      action: "wait",
      context: {
        session_id: "session-1",
        workspace_path: "/workspace",
        cwd: "/workspace/project",
      },
      payload: { job_id: "job-1" },
    })
    expect(typeof result).toBe("object")
    if (typeof result === "string") {
      throw new TypeError("expected structured tool result")
    }
    expect(result.metadata).toEqual(checkpointMetadata(waitResponse()))
  })

  test("returns a structured error when the version handshake is incompatible", async () => {
    const calls: Request[] = []
    const executor: CliExecutor = {
      async execute(request) {
        calls.push(request)
        return response({
          ...versionResponse(),
          result: { ...versionResponse().result, product: "other-binary" },
        })
      },
    }
    const controller = await createManagedBashController({ executor })

    const result = await controller.execute({ action: "run", command: "printf ok" }, toolContext())

    expect(calls).toHaveLength(1)
    expect(typeof result).toBe("object")
    if (typeof result === "string") {
      throw new TypeError("expected structured tool result")
    }
    expect(result.output).toContain("runner_protocol_error")
  })

  test("rejects a binary from a different release", async () => {
    // Given
    const calls: Request[] = []
    const executor: CliExecutor = {
      async execute(request) {
        calls.push(request)
        return response({
          ...versionResponse(),
          result: { ...versionResponse().result, binary_version: "0.1.0" },
        })
      },
    }
    const controller = await createManagedBashController({
      executor,
      pluginVersion: "0.2.0",
    })

    // When
    const result = await controller.execute({ action: "run", command: "printf ok" }, toolContext())

    // Then
    expect(calls).toHaveLength(1)
    expect(typeof result).toBe("object")
    if (typeof result === "string") {
      throw new TypeError("expected structured tool result")
    }
    expect(result.output).toContain("runner_protocol_error")
  })
})

function toolContext(ask: ToolContext["ask"] = async () => undefined): ToolContext {
  return {
    sessionID: "session-1",
    messageID: "message-1",
    agent: "build",
    directory: "/workspace/project",
    worktree: "/workspace",
    abort: new AbortController().signal,
    metadata: () => undefined,
    ask,
  }
}

function fakeExecutor(calls: Request[]): CliExecutor {
  return {
    async execute(request) {
      calls.push(request)
      switch (request.action) {
        case "version":
          return response(versionResponse())
        case "run":
          return response(runResponse())
        case "wait":
          return response(waitResponse())
        default:
          throw new TypeError(`unexpected action: ${request.action}`)
      }
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
      binary_version: "dev",
      protocol_version: 1,
      os: "linux",
      architecture: "amd64",
    },
  }
}

function runResponse(): Extract<Response, { action: "run" }> {
  return {
    schema_version: 1,
    ok: true,
    action: "run",
    result: {
      job_id: "job-1",
      status: "running",
      owner_session_id: "session-1",
      workspace_path: "/workspace",
      cwd: "/workspace/project",
      command: "printf ok",
      created_at_unix_ms: 1,
      started_at_unix_ms: 1,
      captured_bytes: 0,
      hard_timeout_ms: 7_200_000,
      output_limit_bytes: 104_857_600,
    },
  }
}

function waitResponse(): Extract<Response, { action: "wait" }> {
  return {
    schema_version: 1,
    ok: true,
    action: "wait",
    result: {
      observation: { job: { ...runResponse().result, status: "running" } },
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

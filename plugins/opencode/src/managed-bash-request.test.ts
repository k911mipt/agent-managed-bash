import type { ToolContext } from "@opencode-ai/plugin"
import { describe, expect, test } from "bun:test"
import { requestFor, trustedContextFor } from "./managed-bash-request"

describe("trustedContextFor", () => {
  test("uses the OpenCode worktree for a project session", () => {
    // Given
    const context = toolContext("/workspace", "/workspace/project")

    // When
    const result = trustedContextFor(context)

    // Then
    expect(result).toEqual({ session_id: "session-1", workspace_path: "/workspace", cwd: "/workspace/project" })
  })

  test("uses the session directory when OpenCode reports the global root worktree", () => {
    // Given
    const context = toolContext("/", "/codenv/arcadia/taxi/backend-py3")

    // When
    const result = trustedContextFor(context)

    // Then
    expect(result).toEqual({
      session_id: "session-1",
      workspace_path: "/codenv/arcadia/taxi/backend-py3",
      cwd: "/codenv/arcadia/taxi/backend-py3",
    })
  })
})

describe("requestFor", () => {
  test("maps start and run to distinct launch contracts", () => {
    // Given
    const context = { session_id: "session-1", workspace_path: "/workspace", cwd: "/workspace" }

    // When
    const start = requestFor({ action: "start", command: "sleep 30" }, context)
    const run = requestFor({ action: "run", command: "sleep 30", timeout_ms: 500, idle_timeout_ms: 20 }, context)

    // Then
    expect(start).toEqual({
      schema_version: 1,
      action: "start",
      context,
      payload: { command: "sleep 30" },
    })
    expect(run).toEqual({
      schema_version: 1,
      action: "run",
      context,
      payload: { command: "sleep 30", timeout_ms: 500, idle_timeout_ms: 20 },
    })
  })
})

function toolContext(worktree: string, directory: string): ToolContext {
  return {
    sessionID: "session-1",
    messageID: "message-1",
    agent: "build",
    directory,
    worktree,
    abort: new AbortController().signal,
    metadata: () => undefined,
    ask: async () => undefined,
  }
}

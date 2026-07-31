import { describe, expect, test } from "bun:test"
import { managedBashActionSchema, managedBashToolArgs } from "./action-schema"

describe("managed_bash action schema", () => {
  test("labels action-specific fields consistently with runtime validation", () => {
    // Given
    const runWithIdleTimeout = {
      action: "run",
      command: "printf ok",
      idle_timeout_ms: 500,
    }
    const start = { action: "start", command: "printf ok" }
    const waitWithIdleTimeout = {
      action: "wait",
      job_id: "job-1",
      idle_timeout_ms: 500,
    }

    // When
    const runResult = managedBashActionSchema.safeParse(runWithIdleTimeout)
    const startResult = managedBashActionSchema.safeParse(start)
    const waitResult = managedBashActionSchema.safeParse(waitWithIdleTimeout)

    // Then
    expect(managedBashToolArgs.command.description).toStartWith("[start/run only]")
    expect(managedBashToolArgs.idle_timeout_ms.description).toStartWith("[run/wait only]")
    expect(managedBashToolArgs.start_cursor_bytes.description).toStartWith("[output only]")
    expect(startResult.success).toBeTrue()
    expect(runResult.success).toBeTrue()
    expect(waitResult.success).toBeTrue()
  })
})

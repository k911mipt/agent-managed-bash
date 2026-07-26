import { expect, test } from "bun:test"
import type { WaitResponse } from "./generated/protocol.gen"
import { checkpointMetadata } from "./managed-bash-checkpoint"

test("terminal checkpoint is bounded and excludes output and job internals", () => {
  // Given
  const response = {
    schema_version: 1,
    ok: true,
    action: "wait",
    result: {
      observation: {
        job: {
          job_id: "j".repeat(64),
          status: "succeeded",
          owner_session_id: "session-1",
          workspace_path: "/workspace",
          cwd: "/workspace",
          command: "secret command",
          created_at_unix_ms: 1,
          started_at_unix_ms: 1,
          finished_at_unix_ms: 9_007_199_254_740_991,
          captured_bytes: 104_857_600,
          hard_timeout_ms: 86_400_000,
          output_limit_bytes: 104_857_600,
        },
        process_result: {
          status: "succeeded",
          finished_at_unix_ms: 9_007_199_254_740_991,
          captured_bytes: 104_857_600,
          exit_code: 0,
        },
      },
      output: {
        text: "secret log output",
        start_cursor_bytes: 104_857_599,
        next_cursor_bytes: 104_857_600,
        captured_bytes: 104_857_600,
        eof: true,
      },
    },
  } satisfies WaitResponse

  // When
  const metadata = checkpointMetadata(response)

  // Then
  const serialized = JSON.stringify(metadata)
  expect(new TextEncoder().encode(serialized).byteLength).toBeLessThanOrEqual(512)
  expect(serialized).not.toContain("secret log output")
  expect(serialized).not.toContain("secret command")
  expect(metadata).toEqual({
    managed_bash_checkpoint: {
      schema_version: 1,
      event: "wait_checkpoint",
      job_id: "j".repeat(64),
      status: "succeeded",
      captured_bytes: 104_857_600,
      start_cursor_bytes: 104_857_599,
      next_cursor_bytes: 104_857_600,
      finished_at_unix_ms: 9_007_199_254_740_991,
    },
  })
})

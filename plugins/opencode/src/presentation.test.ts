import { expect, test } from "bun:test"
import type { Response } from "./generated/protocol.gen"
import { formatProtocolResponse } from "./presentation"

test("wait output reports the persisted cursor interval", () => {
  const result = formatProtocolResponse({
    schema_version: 1,
    ok: true,
    action: "wait",
    result: {
      observation: {
        job: {
          job_id: "job-1",
          status: "running",
          owner_session_id: "session-1",
          workspace_path: "/workspace",
          cwd: "/workspace",
          command: "sleep 1",
          created_at_unix_ms: 1,
          started_at_unix_ms: 1,
          captured_bytes: 20,
          hard_timeout_ms: 7_200_000,
          output_limit_bytes: 104_857_600,
        },
      },
      output: {
        text: "new output",
        start_cursor_bytes: 10,
        next_cursor_bytes: 20,
        captured_bytes: 20,
        eof: false,
      },
    },
  } satisfies Response)

  expect(typeof result).toBe("object")
  if (typeof result === "string") {
    throw new TypeError("expected structured tool result")
  }
  expect(result.output).toContain("cursor 10→20/20")
})

test("preserves exactly 200 newline-terminated output lines", () => {
  const lines = Array.from({ length: 200 }, (_, index) => `line-${index + 1}`)
  const result = formatProtocolResponse(waitResponse(`${lines.join("\n")}\n`))

  expect(typeof result).toBe("object")
  if (typeof result === "string") {
    throw new TypeError("expected structured tool result")
  }
  expect(result.output).toContain("line-1")
  expect(result.output).toContain("line-200")
  expect(result.output).not.toContain("earlier lines omitted")
})

function waitResponse(text: string): Response {
  return {
    schema_version: 1,
    ok: true,
    action: "wait",
    result: {
      observation: {
        job: {
          job_id: "job-1",
          status: "running",
          owner_session_id: "session-1",
          workspace_path: "/workspace",
          cwd: "/workspace",
          command: "sleep 1",
          created_at_unix_ms: 1,
          started_at_unix_ms: 1,
          captured_bytes: text.length,
          hard_timeout_ms: 7_200_000,
          output_limit_bytes: 104_857_600,
        },
      },
      output: {
        text,
        start_cursor_bytes: 0,
        next_cursor_bytes: text.length,
        captured_bytes: text.length,
        eof: false,
      },
    },
  }
}

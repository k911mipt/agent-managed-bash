import type { ToolResult } from "@opencode-ai/plugin"
import { CliProtocolError } from "./cli-transport"
import { formatToolError } from "./presentation"

export function formatExecutionError(error: unknown): ToolResult {
  if (error instanceof CliProtocolError) {
    return formatToolError("runner_protocol_error", error.message)
  }
  if (error instanceof DOMException && error.name === "AbortError") {
    return formatToolError("runner_aborted", error.message)
  }
  return formatToolError("runner_transport_error", error instanceof Error ? error.message : "unknown runner failure")
}

import type { ToolResult } from "@opencode-ai/plugin"
import type { ErrorDetails, JobObservation, OutputChunk, Response } from "./generated/protocol.gen"
import { checkpointMetadata } from "./managed-bash-checkpoint"

const responseLineLimit = 200
const errorDetailValueLimit = 256

export function formatProtocolResponse(response: Response): ToolResult {
  if (!response.ok) {
    return formatToolError(response.error.code, response.error.message, response.error.details)
  }

  switch (response.action) {
    case "run":
      return result(`job ${response.result.job_id}: ${response.result.status}`)
    case "wait":
      return {
        title: "managed_bash",
        output: formatOutputObservation(response.result.observation, response.result.output),
        metadata: checkpointMetadata(response),
      }
    case "status":
      return result(formatObservation(response.result))
    case "output":
      return result(formatOutputObservation(response.result.observation, response.result.output))
    case "cancel":
      return result(`job ${response.result.job_id}: ${response.result.outcome} (${response.result.status})`)
    case "remove":
      return result(`job ${response.result.job_id}: removed`)
    case "list":
      return result(formatList(response.result.jobs))
    case "version":
      return result(`${response.result.product} ${response.result.binary_version} (protocol v${response.result.protocol_version})`)
    default:
      return assertNever(response)
  }
}

export function formatToolError(code: string, message: string, details?: ErrorDetails): ToolResult {
  return {
    title: "managed_bash error",
    output: `${code}: ${message}${formatErrorDetails(details)}`,
    metadata: { code },
  }
}

function formatErrorDetails(details: ErrorDetails | undefined): string {
  if (details === undefined) {
    return ""
  }
  const values = [
    detailEntry("field", details.field),
    detailEntry("reason", details.reason),
    detailEntry("expected", details.expected),
    detailEntry("actual", details.actual),
  ].filter((entry) => entry !== undefined)
  return values.length === 0 ? "" : ` (${values.join("; ")})`
}

function detailEntry(name: string, value: string | undefined): string | undefined {
  if (value === undefined) {
    return undefined
  }
  const normalized = value.replace(/[\u0000-\u001f\u007f-\u009f\s]+/g, " ").trim()
  const runes = Array.from(normalized)
  const bounded = runes.length <= errorDetailValueLimit ? normalized : `${runes.slice(0, errorDetailValueLimit - 1).join("")}…`
  return `${name}=${bounded}`
}

function result(output: string): ToolResult {
  return { title: "managed_bash", output }
}

function formatObservation(observation: JobObservation, output?: string): string {
  const header = `job ${observation.job.job_id}: ${observation.job.status}`
  if (output === undefined || output === "") {
    return header
  }
  return `${header}\n${tailLines(output)}`
}

function formatOutputObservation(observation: JobObservation, output: OutputChunk): string {
  return `${formatObservation(observation, output.text)}\ncursor ${output.start_cursor_bytes}→${output.next_cursor_bytes}/${output.captured_bytes}${output.eof ? " (eof)" : ""}`
}

function formatList(jobs: readonly JobObservation["job"][]): string {
  if (jobs.length === 0) {
    return "no jobs"
  }
  return jobs.map((job) => `job ${job.job_id}: ${job.status}`).join("\n")
}

function tailLines(output: string): string {
  const newlineTerminated = output.endsWith("\n")
  const lines = (newlineTerminated ? output.slice(0, -1) : output).split("\n")
  if (lines.length <= responseLineLimit) {
    return output
  }
  const omitted = lines.length - responseLineLimit
  return `[${omitted} earlier lines omitted]\n${lines.slice(-responseLineLimit).join("\n")}${newlineTerminated ? "\n" : ""}`
}

function assertNever(value: never): never {
  throw new TypeError(`unreachable response: ${String(value)}`)
}

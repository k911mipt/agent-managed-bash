import { tool } from "@opencode-ai/plugin"

const action = tool.schema.enum([
  "start",
  "run",
  "wait",
  "status",
  "output",
  "cancel",
  "remove",
  "list",
  "version",
])
const jobID = tool.schema.string().min(1)
const positiveInteger = tool.schema.number().int().positive()
const nonNegativeInteger = tool.schema.number().int().nonnegative()

export const managedBashToolArgs = {
  action: action.describe("Operation to perform."),
  command: tool.schema.string().min(1).optional().describe("[start/run only] Non-interactive shell command to start."),
  hard_timeout_ms: positiveInteger.optional().describe("[start/run only] Terminate the process group after this duration."),
  output_limit_bytes: positiveInteger.optional().describe("[start/run only] Terminate the process group at this capture limit."),
  job_id: jobID.optional().describe("[wait/status/output/cancel/remove only] Existing job identifier."),
  cursor_bytes: nonNegativeInteger.optional().describe("[wait only] Output cursor to continue observing from."),
  timeout_ms: positiveInteger.optional().describe("[run/wait only] Return control after this total observation duration."),
  idle_timeout_ms: positiveInteger
    .optional()
    .describe("[run/wait only] Return control after no new output for this duration; does not terminate the job."),
  start_cursor_bytes: nonNegativeInteger.optional().describe("[output only] Inclusive output cursor."),
  end_cursor_bytes: nonNegativeInteger.optional().describe("[output only] Exclusive output cursor."),
} as const

export const managedBashActionSchema = tool.schema.discriminatedUnion("action", [
  tool.schema
    .object({
      action: tool.schema.literal("start"),
      command: tool.schema.string().min(1),
      hard_timeout_ms: positiveInteger.optional(),
      output_limit_bytes: positiveInteger.optional(),
    })
    .strict(),
  tool.schema
    .object({
      action: tool.schema.literal("run"),
      command: tool.schema.string().min(1),
      hard_timeout_ms: positiveInteger.optional(),
      output_limit_bytes: positiveInteger.optional(),
      timeout_ms: positiveInteger.optional(),
      idle_timeout_ms: positiveInteger.optional(),
    })
    .strict(),
  tool.schema
    .object({
      action: tool.schema.literal("wait"),
      job_id: jobID,
      cursor_bytes: nonNegativeInteger.optional(),
      timeout_ms: positiveInteger.optional(),
      idle_timeout_ms: positiveInteger.optional(),
    })
    .strict(),
  tool.schema.object({ action: tool.schema.literal("status"), job_id: jobID }).strict(),
  tool.schema
    .object({
      action: tool.schema.literal("output"),
      job_id: jobID,
      start_cursor_bytes: nonNegativeInteger.optional(),
      end_cursor_bytes: nonNegativeInteger.optional(),
    })
    .strict(),
  tool.schema.object({ action: tool.schema.literal("cancel"), job_id: jobID }).strict(),
  tool.schema.object({ action: tool.schema.literal("remove"), job_id: jobID }).strict(),
  tool.schema.object({ action: tool.schema.literal("list") }).strict(),
  tool.schema.object({ action: tool.schema.literal("version") }).strict(),
])

export type ManagedBashAction = ReturnType<typeof managedBashActionSchema.parse>

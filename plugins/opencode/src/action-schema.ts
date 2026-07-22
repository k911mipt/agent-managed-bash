import { tool } from "@opencode-ai/plugin"

const action = tool.schema.enum([
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
  action,
  command: tool.schema.string().min(1).optional(),
  hard_timeout_ms: positiveInteger.optional(),
  output_limit_bytes: positiveInteger.optional(),
  job_id: jobID.optional(),
  cursor_bytes: nonNegativeInteger.optional(),
  timeout_ms: positiveInteger.optional(),
  idle_timeout_ms: positiveInteger.optional(),
  start_cursor_bytes: nonNegativeInteger.optional(),
  end_cursor_bytes: nonNegativeInteger.optional(),
} as const

export const managedBashActionSchema = tool.schema.discriminatedUnion("action", [
  tool.schema
    .object({
      action: tool.schema.literal("run"),
      command: tool.schema.string().min(1),
      hard_timeout_ms: positiveInteger.optional(),
      output_limit_bytes: positiveInteger.optional(),
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

import type { JobMetadata, OutputChunk, WaitResponse } from "./generated/protocol.gen"

export type ManagedBashCheckpoint = {
  readonly schema_version: 1
  readonly event: "wait_checkpoint"
  readonly job_id: JobMetadata["job_id"]
  readonly status: JobMetadata["status"]
  readonly captured_bytes: OutputChunk["captured_bytes"]
  readonly start_cursor_bytes: OutputChunk["start_cursor_bytes"]
  readonly next_cursor_bytes: OutputChunk["next_cursor_bytes"]
  readonly finished_at_unix_ms?: NonNullable<JobMetadata["finished_at_unix_ms"]>
}

export type ManagedBashCheckpointMetadata = {
  readonly managed_bash_checkpoint: ManagedBashCheckpoint
}

export function checkpointMetadata(response: WaitResponse): ManagedBashCheckpointMetadata {
  const job = response.result.observation.job
  const output = response.result.output
  const checkpoint = {
    schema_version: 1,
    event: "wait_checkpoint",
    job_id: job.job_id,
    status: job.status,
    captured_bytes: output.captured_bytes,
    start_cursor_bytes: output.start_cursor_bytes,
    next_cursor_bytes: output.next_cursor_bytes,
  } as const satisfies ManagedBashCheckpoint

  if (job.finished_at_unix_ms === undefined) {
    return { managed_bash_checkpoint: checkpoint }
  }
  return {
    managed_bash_checkpoint: {
      ...checkpoint,
      finished_at_unix_ms: job.finished_at_unix_ms,
    },
  }
}

import type { ProtocolError, Request, Response } from "./generated/protocol.gen"
import { parseRawJSON } from "./schema-fixture-harness"
import type { ResponseValidator } from "./response-validator"

export type CliExecution = {
  readonly exitCode: number
  readonly stderr: Uint8Array
  readonly stdout: Uint8Array
}

export interface CliExecutor {
  execute(request: Request, signal: AbortSignal): Promise<CliExecution>
}

export class CliProtocolError extends Error {
  constructor(message: string) {
    super(message)
    this.name = "CliProtocolError"
  }
}

export function createBunCliExecutor(executable = process.env["MANAGED_BASH_BINARY"] ?? "managed-bash"): CliExecutor {
  return {
    async execute(request, signal) {
      if (signal.aborted) {
        throw new DOMException("managed_bash request aborted", "AbortError")
      }

      const child = Bun.spawn([executable, request.action], {
        cwd: request.action === "version" ? process.cwd() : request.context.cwd,
        env: trustedEnvironment(request),
        stdin: "pipe",
        stdout: "pipe",
        stderr: "pipe",
      })
      child.stdin.write(JSON.stringify(request))
      child.stdin.end()
      const terminate = (): void => {
        child.kill()
      }
      signal.addEventListener("abort", terminate, { once: true })

      try {
        const [exitCode, stdout, stderr] = await Promise.all([
          child.exited,
          new Response(child.stdout).arrayBuffer(),
          new Response(child.stderr).arrayBuffer(),
        ])
        return {
          exitCode,
          stdout: new Uint8Array(stdout),
          stderr: new Uint8Array(stderr),
        }
      } finally {
        signal.removeEventListener("abort", terminate)
      }
    },
  }
}

export async function executeProtocolRequest(
  executor: CliExecutor,
  request: Request,
  signal: AbortSignal,
  validateResponse: ResponseValidator,
): Promise<Response> {
  const execution = await executor.execute(request, signal)
  const response = parseResponse(execution.stdout, validateResponse)

  if (response.action !== undefined && response.action !== request.action) {
    throw new CliProtocolError(`response action ${response.action} did not match request action ${request.action}`)
  }
  if (response.ok && execution.exitCode !== 0) {
    throw new CliProtocolError(`successful response exited with ${execution.exitCode}`)
  }
  if (!response.ok && execution.exitCode !== exitClassFor(response.error)) {
    throw new CliProtocolError(`error ${response.error.code} exited with ${execution.exitCode}`)
  }

  return response
}

function exitClassFor(error: ProtocolError): number {
  const exitClasses: Record<ProtocolError["code"], number> = {
    malformed_json: 2,
    invalid_request: 2,
    incompatible_version: 2,
    invalid_range: 2,
    invalid_cursor: 2,
    job_not_found: 3,
    unauthorized: 3,
    active_job: 4,
    conflict: 4,
    corrupt_state: 5,
    runner_unavailable: 5,
    io_failure: 5,
    internal: 5,
  }
  return exitClasses[error.code]
}

function trustedEnvironment(request: Request): NodeJS.ProcessEnv {
  if (request.action === "version") {
    return process.env
  }

  return {
    ...process.env,
    MANAGED_BASH_HOST_SESSION_ID: request.context.session_id,
    MANAGED_BASH_HOST_WORKSPACE_PATH: request.context.workspace_path,
  }
}

function parseResponse(raw: Uint8Array, validateResponse: ResponseValidator): Response {
  if (raw.length < 2 || raw.at(-1) !== 10) {
    throw new CliProtocolError("CLI stdout must contain exactly one newline-delimited response")
  }
  const payload = raw.slice(0, -1)
  if (payload.includes(10) || payload.includes(13)) {
    throw new CliProtocolError("CLI stdout contained more than one response line")
  }

  const parsed = parseRawJSON(payload)
  if (parsed.kind === "rejected" || !validateResponse(parsed.value)) {
    throw new CliProtocolError("CLI stdout was not a schema-valid protocol response")
  }
  return parsed.value
}

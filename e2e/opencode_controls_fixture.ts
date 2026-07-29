import { access } from "node:fs/promises"

export type JSONRecord = Record<string, unknown>

export type ToolEvent = {
  readonly callID: string | undefined
  readonly tool: string
  readonly status: string
  readonly action: string | undefined
  readonly output: string | undefined
  readonly error: string | undefined
}

export function toolCalls(calls: readonly { readonly id: string; readonly input: JSONRecord }[]): JSONRecord {
  return {
    id: `chatcmpl-${calls.map((call) => call.id).join("-")}`,
    object: "chat.completion.chunk",
    created: 1,
    model: "fixture",
    choices: [{
      index: 0,
      delta: {
        role: "assistant",
        tool_calls: calls.map((call, index) => ({
          index,
          id: call.id,
          type: "function",
          function: { name: "managed_bash", arguments: JSON.stringify(call.input) },
        })),
      },
      finish_reason: "tool_calls",
    }],
    usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
  }
}

export function textResponse(content: string): JSONRecord {
  return {
    id: "chatcmpl-final",
    object: "chat.completion.chunk",
    created: 1,
    model: "fixture",
    choices: [{ index: 0, delta: { role: "assistant", content }, finish_reason: "stop" }],
    usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
  }
}

export function openAIResponse(value: JSONRecord): Response {
  return new Response(`data: ${JSON.stringify(value)}\n\ndata: [DONE]\n\n`, {
    headers: { "Content-Type": "text/event-stream" },
  })
}

export function extractJobIDs(body: string): readonly string[] {
  return [...body.matchAll(/job ([A-Za-z0-9_-]+):/g)]
    .map((match) => match[1])
    .filter((value): value is string => value !== undefined)
    .filter((value, index, values) => values.indexOf(value) === index)
}

export function extractJobID(body: string, callID: string): string | undefined {
  const request: unknown = JSON.parse(body)
  if (!isRecord(request) || !Array.isArray(request["messages"])) return undefined
  for (const message of request["messages"]) {
    if (!isRecord(message) || message["role"] !== "tool" || message["tool_call_id"] !== callID) continue
    const content = message["content"]
    if (typeof content !== "string") return undefined
    return /job ([A-Za-z0-9_-]+):/.exec(content)?.[1]
  }
  return undefined
}

export function parseToolEvents(output: string): readonly ToolEvent[] {
  const result: ToolEvent[] = []
  for (const line of output.split("\n").filter((value) => value !== "")) {
    const event: unknown = JSON.parse(line)
    if (!isRecord(event) || event["type"] !== "tool_use" || !isRecord(event["part"])) continue
    const part = event["part"]
    if (typeof part["tool"] !== "string" || !isRecord(part["state"])) continue
    const state = part["state"]
    const input = isRecord(state["input"]) ? state["input"] : undefined
    result.push({
      callID: typeof part["callID"] === "string" ? part["callID"] : undefined,
      tool: part["tool"],
      status: typeof state["status"] === "string" ? state["status"] : "unknown",
      action: typeof input?.["action"] === "string" ? input["action"] : undefined,
      output: typeof state["output"] === "string" ? state["output"] : undefined,
      error: typeof state["error"] === "string" ? state["error"] : undefined,
    })
  }
  return result
}

export async function runProcess(
  command: readonly string[],
  env: NodeJS.ProcessEnv,
  cwd: string,
  timeoutMs = 90_000,
): Promise<{ readonly stdout: string; readonly stderr: string }> {
  const child = Bun.spawn([...command], { cwd, env, stdout: "pipe", stderr: "pipe" })
  let timedOut = false
  let forceKill: ReturnType<typeof setTimeout> | undefined
  const timeout = setTimeout(() => {
    timedOut = true
    child.kill("SIGTERM")
    forceKill = setTimeout(() => child.kill("SIGKILL"), 2_000)
  }, timeoutMs)
  const [exitCode, stdout, stderr] = await Promise.all([
    child.exited,
    new Response(child.stdout).text(),
    new Response(child.stderr).text(),
  ]).finally(() => {
    clearTimeout(timeout)
    if (forceKill !== undefined) clearTimeout(forceKill)
  })
  if (timedOut) throw new Error(`${command[0]} exceeded ${timeoutMs}ms`)
  if (exitCode !== 0) throw new Error(`${command[0]} exited ${exitCode}\nstdout:\n${stdout}\nstderr:\n${stderr}`)
  return { stdout, stderr }
}

export async function waitForFile(path: string, timeoutMs = 5_000): Promise<void> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      await access(path)
      return
    } catch (error: unknown) {
      if (!(error instanceof Error) || !("code" in error) || error.code !== "ENOENT") throw error
    }
    await Bun.sleep(25)
  }
  throw new Error(`file did not appear: ${path}`)
}

export async function waitForProcessExit(pid: number, timeoutMs = 5_000): Promise<void> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      process.kill(pid, 0)
    } catch (error: unknown) {
      if (error instanceof Error && "code" in error && error.code === "ESRCH") return
      throw error
    }
    await Bun.sleep(25)
  }
  throw new Error(`process remained alive: ${pid}`)
}

export function requireCondition(condition: boolean, message: string): asserts condition {
  if (!condition) throw new Error(message)
}

function isRecord(value: unknown): value is JSONRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

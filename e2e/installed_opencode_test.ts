import { mkdir, mkdtemp, realpath, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { pathToFileURL } from "node:url"

type JSONRecord = Record<string, unknown>

type ToolEvent = {
  readonly tool: string
  readonly action: string | undefined
  readonly output: string
}

const checkpointCommand = "while [ ! -f checkpoint-release ]; do sleep 0.05; done; printf e2e-complete"
const shortCommand = "printf e2e-short"
const processTimeoutMs = 90_000

const bundleRoot = requiredArgument(2, "bundle root")
const opencode = requiredArgument(3, "OpenCode executable")

const root = await realpath(await mkdtemp(join(tmpdir(), "managed-bash-opencode-e2e-")))
try {
  await runScenario("success", false)
  await runScenario("failure", true)
} finally {
  await rm(root, { force: true, recursive: true })
}

async function runScenario(name: string, missingBinary: boolean): Promise<void> {
  const scenarioRoot = join(root, name)
  const home = join(scenarioRoot, "home")
  const configHome = join(home, ".config")
  const dataHome = join(home, ".local", "share")
  const cacheHome = join(home, ".cache")
  const stateHome = join(home, ".local", "state")
  const binDir = join(home, ".local", "bin")
  const workspace = join(scenarioRoot, "workspace")
  await Promise.all([
    mkdir(configHome, { recursive: true }),
    mkdir(dataHome, { recursive: true }),
    mkdir(cacheHome, { recursive: true }),
    mkdir(stateHome, { recursive: true }),
    mkdir(binDir, { recursive: true }),
    mkdir(workspace, { recursive: true }),
  ])

  const env: NodeJS.ProcessEnv = {
    ...process.env,
    HOME: home,
    XDG_CONFIG_HOME: configHome,
    XDG_DATA_HOME: dataHome,
    XDG_CACHE_HOME: cacheHome,
    XDG_STATE_HOME: stateHome,
    MANAGED_BASH_BIN_DIR: binDir,
    PATH: `${binDir}:/usr/bin:/bin`,
  }
  delete env["MANAGED_BASH_BINARY"]
  await run(["sh", join(bundleRoot, "install.sh")], env, scenarioRoot)
  requireCondition(!(await Bun.file(join(configHome, "opencode", "plugins", "managed-bash.js")).exists()), `${name}: installer created an auto-discovery plugin`)
  await run(["git", "init", "--quiet", "--separate-git-dir", join(scenarioRoot, "git-dir"), workspace], env, scenarioRoot)

  let requestCount = 0
  let jobID: string | undefined
  const server = Bun.serve({
    hostname: "127.0.0.1",
    port: 0,
    async fetch(request) {
      if (!request.url.endsWith("/chat/completions")) {
        return new Response("not found", { status: 404 })
      }
      requestCount += 1
      const body = await request.text()
      const observedJobID = /job ([A-Za-z0-9_-]+):/.exec(body)?.[1]
      if (observedJobID !== undefined) {
        jobID = observedJobID
      }
      if (!missingBinary && requestCount === 2) {
        await writeFile(join(workspace, "checkpoint-release"), "")
      }
      return openAIResponse(responseFor(requestCount, missingBinary, jobID))
    },
  })

  try {
    const serverPort = server.port
    if (serverPort === undefined) throw new TypeError("fixture server did not bind a TCP port")
    await writeConfig(configHome, dataHome, serverPort)
    const runEnv = missingBinary
      ? { ...env, MANAGED_BASH_BINARY: join(scenarioRoot, "missing-managed-bash") }
      : env
    const execution = await run(
      [
        opencode,
        "run",
        "--dir",
        workspace,
        "--agent",
        "e2e",
        "--model",
        "e2e/fixture",
        "--format",
        "json",
        "--title",
        `managed-bash-${name}`,
        missingBinary ? "Call managed_bash version once." : "Run the managed bash checkpoint scenario.",
      ],
      runEnv,
      workspace,
    )
    const events = parseJSONLines(execution.stdout)
    const tools = toolEvents(events)
    requireCondition(tools.length > 0, `${name}: no completed tool events`)
    requireCondition(tools.every((event) => event.tool !== "bash"), `${name}: built-in bash was used`)
    requireCondition(tools.every((event) => event.tool === "managed_bash"), `${name}: unexpected tool was used`)
    requireCondition(!/unknown agent|plugin[^\n]*(failed|error)/i.test(execution.stderr), `${name}: OpenCode reported a load error`)

    if (missingBinary) {
      requireCondition(tools.length === 1, "failure: expected exactly one managed_bash call")
      requireCondition(tools[0]?.output.includes("runner_transport_error") === true, "failure: missing structured transport error")
      return
    }

    requireCondition(requestCount === 4, `success: expected four model requests, got ${requestCount}`)
    requireCondition(tools[0]?.output.includes("return reason: output idle checkpoint") === true, "success: run did not return an idle checkpoint")
    requireCondition(tools[1]?.output.includes(": succeeded") === true, "success: wait did not complete the checkpoint job")
    requireCondition(tools[1]?.output.includes("e2e-complete") === true, "success: checkpoint output was missing")
    requireCondition(tools[2]?.output.includes(": succeeded") === true, "success: short run did not complete in one call")
    requireCondition(tools[2]?.output.includes("e2e-short") === true, "success: short run output was missing")
  } finally {
    await writeFile(join(workspace, "checkpoint-release"), "")
    server.stop(true)
    await run(["sh", join(bundleRoot, "uninstall.sh")], env, scenarioRoot)
  }
}

function responseFor(count: number, missingBinary: boolean, jobID: string | undefined): JSONRecord {
  if (missingBinary) {
    return count === 1
      ? toolCall("call-version", { action: "version" })
      : textResponse("structured failure observed")
  }
  switch (count) {
    case 1:
      return toolCall("call-run-idle", { action: "run", command: checkpointCommand, hard_timeout_ms: 15_000, timeout_ms: 2000, idle_timeout_ms: 100 })
    case 2:
      return toolCall("call-wait-complete", { action: "wait", job_id: requireJobID(jobID), timeout_ms: 10000, idle_timeout_ms: 10000 })
    case 3:
      return toolCall("call-run-short", { action: "run", command: shortCommand })
    default:
      return textResponse("managed bash checkpoint scenario complete")
  }
}

function toolCall(id: string, input: JSONRecord): JSONRecord {
  return {
    id: `chatcmpl-${id}`,
    object: "chat.completion.chunk",
    created: 1,
    model: "fixture",
    choices: [{ index: 0, delta: { role: "assistant", tool_calls: [{ index: 0, id, type: "function", function: { name: "managed_bash", arguments: JSON.stringify(input) } }] }, finish_reason: "tool_calls" }],
    usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
  }
}

function textResponse(content: string): JSONRecord {
  return {
    id: "chatcmpl-final",
    object: "chat.completion.chunk",
    created: 1,
    model: "fixture",
    choices: [{ index: 0, delta: { role: "assistant", content }, finish_reason: "stop" }],
    usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
  }
}

function openAIResponse(value: JSONRecord): Response {
  return new Response(`data: ${JSON.stringify(value)}\n\ndata: [DONE]\n\n`, {
    headers: { "Content-Type": "text/event-stream" },
  })
}

async function writeConfig(configHome: string, dataHome: string, port: number): Promise<void> {
  const directory = join(configHome, "opencode")
  await mkdir(directory, { recursive: true })
  const config = {
    $schema: "https://opencode.ai/config.json",
    autoupdate: false,
    share: "disabled",
    snapshot: false,
    plugin: [pathToFileURL(join(dataHome, "agent-managed-bash", "current", "lib", "opencode", "managed-bash.js")).href],
    tools: { bash: false },
    provider: {
      e2e: {
        npm: "@ai-sdk/openai-compatible",
        name: "E2E fixture",
        options: { apiKey: "fixture-key", baseURL: `http://127.0.0.1:${port}/v1` },
        models: { fixture: { name: "Fixture", limit: { context: 8192, output: 2048 } } },
      },
    },
    agent: {
      e2e: {
        description: "Installed managed_bash E2E agent",
        mode: "primary",
        model: "e2e/fixture",
        tools: { bash: false, managed_bash: true },
        permission: { bash: { "*": "deny", [checkpointCommand]: "allow", [shortCommand]: "allow" } },
      },
    },
  }
  await writeFile(join(directory, "opencode.json"), `${JSON.stringify(config)}\n`, { mode: 0o600 })
}

async function run(command: readonly string[], env: NodeJS.ProcessEnv, cwd: string): Promise<{ readonly stdout: string; readonly stderr: string }> {
  const child = Bun.spawn([...command], { cwd, env, stdout: "pipe", stderr: "pipe" })
  let timedOut = false
  let forceKill: ReturnType<typeof setTimeout> | undefined
  const timeout = setTimeout(() => {
    timedOut = true
    child.kill("SIGTERM")
    forceKill = setTimeout(() => child.kill("SIGKILL"), 2_000)
  }, processTimeoutMs)
  const [exitCode, stdout, stderr] = await Promise.all([child.exited, new Response(child.stdout).text(), new Response(child.stderr).text()])
    .finally(() => {
      clearTimeout(timeout)
      if (forceKill !== undefined) clearTimeout(forceKill)
    })
  if (timedOut) throw new Error(`${command[0]} exceeded ${processTimeoutMs}ms`)
  if (exitCode !== 0) {
    throw new Error(`${command[0]} exited ${exitCode}\nstdout:\n${stdout}\nstderr:\n${stderr}`)
  }
  return { stdout, stderr }
}

function parseJSONLines(output: string): readonly JSONRecord[] {
  return output.split("\n").filter((line) => line !== "").map((line) => {
    const value: unknown = JSON.parse(line)
    if (!isRecord(value)) throw new TypeError("OpenCode emitted a non-object JSONL event")
    return value
  })
}

function toolEvents(events: readonly JSONRecord[]): readonly ToolEvent[] {
  const result: ToolEvent[] = []
  for (const event of events) {
    if (event["type"] !== "tool_use" || !isRecord(event["part"])) continue
    const part = event["part"]
    if (typeof part["tool"] !== "string" || !isRecord(part["state"])) continue
    const state = part["state"]
    if (state["status"] !== "completed" || typeof state["output"] !== "string") continue
    const input = isRecord(state["input"]) ? state["input"] : undefined
    result.push({ tool: part["tool"], action: typeof input?.["action"] === "string" ? input["action"] : undefined, output: state["output"] })
  }
  return result
}

function isRecord(value: unknown): value is JSONRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function requireJobID(jobID: string | undefined): string {
  if (jobID === undefined) throw new TypeError("fixture did not observe a managed_bash job ID")
  return jobID
}

function requireCondition(condition: boolean, message: string): asserts condition {
  if (!condition) throw new Error(message)
}

function requiredArgument(index: number, name: string): string {
  const value = process.argv[index]
  if (value === undefined) throw new TypeError(`${name} is required`)
  return value
}

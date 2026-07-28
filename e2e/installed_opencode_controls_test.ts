import { mkdir, mkdtemp, readFile, realpath, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { pathToFileURL } from "node:url"
import {
  extractJobIDs,
  openAIResponse,
  parseToolEvents,
  requireCondition,
  runProcess,
  textResponse,
  toolCalls,
  type JSONRecord,
  type ToolEvent,
  waitForFile,
  waitForProcessExit,
} from "./opencode_controls_fixture"

type ScenarioResult = {
  readonly events: readonly ToolEvent[]
  readonly requests: number
  readonly workspace: string
}

const bundleRoot = requiredArgument(2, "bundle root")
const opencode = requiredArgument(3, "OpenCode executable")
const root = await realpath(await mkdtemp(join(tmpdir(), "managed-bash-opencode-controls-")))
const home = join(root, "home")
const configHome = join(home, ".config")
const dataHome = join(home, ".local", "share")
const binDir = join(home, ".local", "bin")
const env: NodeJS.ProcessEnv = {
  ...process.env,
  HOME: home,
  XDG_CONFIG_HOME: configHome,
  XDG_DATA_HOME: dataHome,
  XDG_CACHE_HOME: join(home, ".cache"),
  XDG_STATE_HOME: join(home, ".local", "state"),
  MANAGED_BASH_BIN_DIR: binDir,
  PATH: `${binDir}:/usr/bin:/bin`,
}
delete env["MANAGED_BASH_BINARY"]

try {
  await Promise.all([
    mkdir(configHome, { recursive: true }),
    mkdir(dataHome, { recursive: true }),
    mkdir(binDir, { recursive: true }),
    mkdir(env["XDG_CACHE_HOME"] ?? "", { recursive: true }),
    mkdir(env["XDG_STATE_HOME"] ?? "", { recursive: true }),
  ])
  await runProcess(["sh", join(bundleRoot, "install.sh")], env, root)
  requireCondition(!(await Bun.file(join(configHome, "opencode", "plugins", "managed-bash.js")).exists()), "installer created an auto-discovery plugin")
  await parallelCancelScenario()
  await hardTimeoutScenario()
  await cursorScenario()
  await permissionDenialScenario()
} finally {
  await runProcess(["sh", join(bundleRoot, "uninstall.sh")], env, root)
  await rm(root, { force: true, recursive: true })
}

async function parallelCancelScenario(): Promise<void> {
  const firstCommand = "trap '' TERM; sleep 300 & child=$!; printf '%s %s' \"$$\" \"$child\" > pids; wait"
  const secondCommand = "while [ ! -f release-second ]; do sleep 0.05; done; printf second-complete"
  const result = await executeScenario("parallel", [firstCommand, secondCommand], async (count, body, workspace) => {
    const jobs = extractJobIDs(body)
    if (count === 1) return toolCalls([
      { id: "run-first", input: { action: "run", command: firstCommand, hard_timeout_ms: 15_000 } },
      { id: "run-second", input: { action: "run", command: secondCommand, hard_timeout_ms: 15_000 } },
    ])
    requireCondition(jobs.length === 2, `parallel: expected two jobs, got ${jobs.length}`)
    if (count === 2) {
      await waitForFile(join(workspace, "pids"))
      return toolCalls(jobs.map((job, index) => ({
        id: `idle-${index}`,
        input: { action: "wait", job_id: job, timeout_ms: 2_000, idle_timeout_ms: 100 },
      })))
    }
    if (count === 3) {
      await writeFile(join(workspace, "release-second"), "")
      return toolCalls([
        { id: "cancel-first", input: { action: "cancel", job_id: jobs[0] } },
        { id: "finish-second", input: { action: "wait", job_id: jobs[1], timeout_ms: 10_000, idle_timeout_ms: 10_000 } },
      ])
    }
    if (count === 4) return toolCalls([
      { id: "finish-first", input: { action: "wait", job_id: jobs[0], timeout_ms: 15_000, idle_timeout_ms: 10_000 } },
    ])
    return textResponse("parallel controls complete")
  })
  requireActionCounts(result, { run: 2, wait: 4, cancel: 1 })
  requireCondition(result.events.some((event) => event.output?.includes(": cancelled")), "parallel: cancelled status missing")
  requireCondition(result.events.some((event) => event.output?.includes("second-complete")), "parallel: completion missing")
  const pids = (await readFile(join(result.workspace, "pids"), "utf8")).trim().split(" ").map(Number)
  for (const pid of pids) await waitForProcessExit(pid)
}

async function hardTimeoutScenario(): Promise<void> {
  const command = "trap '' TERM; exec sleep 300"
  const result = await executeScenario("hard-timeout", [command], async (count, body) => {
    const job = extractJobIDs(body)[0]
    if (count === 1) return toolCalls([{ id: "timeout-run", input: { action: "run", command, hard_timeout_ms: 200 } }])
    if (body.includes(": hard_timeout")) return textResponse("hard timeout observed")
    requireCondition(count === 2, "hard-timeout: terminal state was not published")
    return toolCalls([{ id: "timeout-wait", input: { action: "wait", job_id: required(job), timeout_ms: 15_000, idle_timeout_ms: 15_000 } }])
  })
  requireCondition(
    result.events.some((event) => event.output?.includes("hard_timeout")),
    `hard-timeout: status missing in ${JSON.stringify(result.events.map((event) => event.output))}`,
  )
}

async function cursorScenario(): Promise<void> {
  const command = "printf abcdef"
  const result = await executeScenario("cursor", [command], async (count, body) => {
    const job = extractJobIDs(body)[0]
    if (count === 1) return toolCalls([{ id: "cursor-run", input: { action: "run", command } }])
    if (count === 2) return toolCalls([{ id: "cursor-wait", input: { action: "wait", job_id: required(job), timeout_ms: 5_000, idle_timeout_ms: 5_000 } }])
    if (count === 3) return toolCalls([{ id: "cursor-output", input: { action: "output", job_id: required(job), start_cursor_bytes: 1, end_cursor_bytes: 4 } }])
    return textResponse("cursor observed")
  })
  requireActions(result, "run,wait,output")
  requireCondition(result.events.at(-1)?.output?.includes("bcd") === true, "cursor: continuation output missing")
}

async function permissionDenialScenario(): Promise<void> {
  const marker = "denied-marker"
  const command = `touch ${marker}`
  const result = await executeScenario("permission", [], async (count) => count === 1
    ? toolCalls([{ id: "denied-run", input: { action: "run", command } }])
    : textResponse("permission denial observed"))
  requireCondition(result.events.length === 1, "permission: expected one tool event")
  requireCondition(result.events[0]?.status === "error", "permission: tool call was not denied")
  requireCondition(result.events[0]?.error?.includes("prevents") === true, "permission: denial message missing")
  requireCondition(!(await Bun.file(join(result.workspace, marker)).exists()), "permission: denied command spawned")
}

async function executeScenario(
  name: string,
  commands: readonly string[],
  response: (count: number, body: string, workspace: string) => Promise<JSONRecord>,
): Promise<ScenarioResult> {
  const workspace = join(root, name)
  await mkdir(workspace)
  await runProcess(["git", "init", "--quiet", "--separate-git-dir", join(root, `${name}-git`), workspace], env, root)
  let requests = 0
  const server = Bun.serve({ hostname: "127.0.0.1", port: 0, async fetch(request) {
    requests += 1
    return openAIResponse(await response(requests, await request.text(), workspace))
  } })
  try {
    requireCondition(server.port !== undefined, `${name}: server port missing`)
    await writeConfig(server.port, commands)
    const execution = await runProcess([
      opencode, "run", "--dir", workspace, "--agent", "e2e", "--model", "e2e/fixture",
      "--format", "json", "--title", `managed-bash-${name}`, `Execute the ${name} fixture.`,
    ], env, workspace)
    const events = parseToolEvents(execution.stdout)
    requireCondition(events.every((event) => event.tool === "managed_bash"), `${name}: unexpected tool used`)
    return { events, requests, workspace }
  } finally {
    server.stop(true)
  }
}

async function writeConfig(port: number, commands: readonly string[]): Promise<void> {
  const permissions: Record<string, "allow" | "deny"> = { "*": "deny" }
  for (const command of commands) permissions[command] = "allow"
  const config = {
    $schema: "https://opencode.ai/config.json", autoupdate: false, share: "disabled", snapshot: false,
    plugin: [pathToFileURL(join(dataHome, "agent-managed-bash", "current", "lib", "opencode", "managed-bash.js")).href],
    tools: { bash: false },
    provider: { e2e: { npm: "@ai-sdk/openai-compatible", name: "E2E fixture", options: { apiKey: "fixture-key", baseURL: `http://127.0.0.1:${port}/v1` }, models: { fixture: { name: "Fixture", limit: { context: 8192, output: 2048 } } } } },
    agent: { e2e: { description: "Installed controls E2E", mode: "primary", model: "e2e/fixture", tools: { bash: false, managed_bash: true }, permission: { bash: permissions } } },
  }
  await mkdir(join(configHome, "opencode"), { recursive: true })
  await writeFile(join(configHome, "opencode", "opencode.json"), `${JSON.stringify(config)}\n`, { mode: 0o600 })
}

function requireActions(result: ScenarioResult, expected: string): void {
  requireCondition(result.events.map((event) => event.action).join(",") === expected, `unexpected actions: ${result.events.map((event) => event.action).join(",")}`)
}

function requireActionCounts(result: ScenarioResult, expected: Readonly<Record<string, number>>): void {
  for (const [action, count] of Object.entries(expected)) {
    const actual = result.events.filter((event) => event.action === action).length
    requireCondition(actual === count, `unexpected ${action} count: ${actual}`)
  }
}

function required(value: string | undefined): string {
  if (value === undefined) throw new Error("fixture did not observe a job ID")
  return value
}

function requiredArgument(index: number, name: string): string {
  const value = process.argv[index]
  if (value === undefined) throw new TypeError(`${name} is required`)
  return value
}

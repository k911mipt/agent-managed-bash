import { readFile, writeFile } from "node:fs/promises"
import { join } from "node:path"

const pollMilliseconds = 5

export type ProcessStateReader = (pid: number) => string | undefined

function readProcessState(pid: number): string | undefined {
  const process = Bun.spawnSync(["ps", "-o", "stat=", "-p", String(pid)])
  if (!process.success) return undefined
  const state = process.stdout?.toString().trim()
  return state === "" || state === undefined ? undefined : state
}

function isStoppedProcessState(state: string | undefined): boolean {
  return state === undefined || state.startsWith("Z")
}

function isMissingFile(error: unknown): boolean {
  return error instanceof Error && "code" in error && error.code === "ENOENT"
}

export async function waitForStoppedProcess(pid: number, timeoutMilliseconds: number, readState: ProcessStateReader = readProcessState): Promise<void> {
  const deadline = Date.now() + timeoutMilliseconds
  for (;;) {
    const state = readState(pid)
    if (isStoppedProcessState(state)) return
    if (Date.now() >= deadline) throw new Error(`process ${pid} remained running with state ${state}`)
    await Bun.sleep(pollMilliseconds)
  }
}

export async function waitForTextFile(path: string, timeoutMilliseconds: number): Promise<string> {
  const deadline = Date.now() + timeoutMilliseconds
  for (;;) {
    try {
      return await readFile(path, "utf8")
    } catch (error) {
      if (!isMissingFile(error)) throw error
    }
    if (Date.now() >= deadline) throw new Error(`timed out waiting for ${path}`)
    await Bun.sleep(pollMilliseconds)
  }
}

export function killProcessGroup(pid: number, signal: "SIGTERM" | "SIGKILL"): void {
  try {
    process.kill(-pid, signal)
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error
  }
}

export async function createTermIgnoringPipeHolder(root: string): Promise<{
  readonly childReadyMarker: string
  readonly childMarker: string
  readonly script: string
}> {
  const childMarker = join(root, "child.pid")
  const childReadyMarker = join(root, "child.ready")
  const script = join(root, "hang.ts")
  const child = "process.on('SIGTERM', () => {}); await Bun.write(process.env.CHILD_READY_MARKER, 'ready'); setInterval(() => {}, 1000)"
  const parent = `const child = Bun.spawn({ cmd: [process.execPath, "-e", ${JSON.stringify(child)}], stderr: "inherit", stdout: "inherit" }); await Bun.write(process.env.MARKER, String(child.pid)); process.stdout.write("pipe-holder-ready\\n"); await new Promise(() => {})`
  await writeFile(script, parent)
  return { childReadyMarker, childMarker, script }
}

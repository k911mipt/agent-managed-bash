export type ReleaseCommandResult =
  | { readonly kind: "completed"; readonly exitCode: number; readonly stderr: string; readonly stdout: string }
  | { readonly kind: "timed_out"; readonly stderr: string; readonly stdout: string }

export type ReleaseCommandRequest = {
  readonly arguments: readonly string[]
  readonly currentDirectory?: string
  readonly environment?: Readonly<Record<string, string>>
  readonly executable: string
  readonly inheritEnvironment?: boolean
  readonly timeoutMilliseconds: number
}

function killGroup(pid: number, signal: "SIGTERM" | "SIGKILL"): void {
  try {
    process.kill(-pid, signal)
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error
  }
}

export async function runReleaseCommand(request: ReleaseCommandRequest): Promise<ReleaseCommandResult> {
  const environment = request.inheritEnvironment === false ? request.environment : { ...process.env, ...request.environment }
  const child = environment === undefined
    ? request.currentDirectory === undefined
      ? Bun.spawn([request.executable, ...request.arguments], { detached: true, stderr: "pipe", stdout: "pipe" })
      : Bun.spawn([request.executable, ...request.arguments], { cwd: request.currentDirectory, detached: true, stderr: "pipe", stdout: "pipe" })
    : request.currentDirectory === undefined
      ? Bun.spawn([request.executable, ...request.arguments], { detached: true, env: environment, stderr: "pipe", stdout: "pipe" })
      : Bun.spawn([request.executable, ...request.arguments], { cwd: request.currentDirectory, detached: true, env: environment, stderr: "pipe", stdout: "pipe" })
  if (typeof child.stderr === "number" || typeof child.stdout === "number" || child.stderr === undefined || child.stdout === undefined) throw new Error("release command output is not piped")
  let timedOut = false
  const term = setTimeout(() => {
    timedOut = true
    killGroup(child.pid, "SIGTERM")
  }, request.timeoutMilliseconds)
  const kill = setTimeout(() => {
    if (timedOut) killGroup(child.pid, "SIGKILL")
  }, request.timeoutMilliseconds + 25)
  const [exitCode, stderr, stdout] = await Promise.all([child.exited, new Response(child.stderr).text(), new Response(child.stdout).text()])
  clearTimeout(term)
  clearTimeout(kill)
  if (timedOut) return { kind: "timed_out", stderr, stdout }
  return { kind: "completed", exitCode, stderr, stdout }
}

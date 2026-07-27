import { mkdir, mkdtemp, rename, rm, writeFile } from "node:fs/promises"
import { dirname, join } from "node:path"

type InterruptSignal = "SIGINT" | "SIGTERM"

export type ReleaseTransaction = {
  atomicDirectory: (output: string, prefix: string, write: (stage: string) => Promise<void>) => Promise<void>
  atomicFile: (output: string, prefix: string, content: string) => Promise<void>
}

class Transaction implements ReleaseTransaction {
  readonly #children = new Set<ReturnType<typeof Bun.spawn>>()
  readonly #rollbackPaths = new Set<string>()
  readonly #stages = new Set<string>()

  async atomicDirectory(output: string, prefix: string, write: (stage: string) => Promise<void>): Promise<void> {
    if (await Bun.file(output).exists()) {
      throw new Error("candidate output already exists")
    }
    await mkdir(dirname(output), { recursive: true })
    const stage = await mkdtemp(join(dirname(output), prefix))
    this.#stages.add(stage)
    await pauseForInterruptionTest(this, stage)
    await write(stage)
    await rename(stage, output)
    this.#stages.delete(stage)
    this.#rollbackPaths.add(output)
  }

  async atomicFile(output: string, prefix: string, content: string): Promise<void> {
    if (await Bun.file(output).exists()) {
      throw new Error("control output already exists")
    }
    await mkdir(dirname(output), { recursive: true })
    const stage = await mkdtemp(join(dirname(output), prefix))
    const stagedFile = join(stage, "output")
    this.#stages.add(stage)
    await pauseForInterruptionTest(this, stage)
    await writeFile(stagedFile, content)
    await rename(stagedFile, output)
    await rm(stage, { force: true, recursive: true })
    this.#stages.delete(stage)
    this.#rollbackPaths.add(output)
  }

  addChild(child: ReturnType<typeof Bun.spawn>): void {
    this.#children.add(child)
  }

  async cleanup(): Promise<void> {
    for (const child of this.#children) {
      child.kill("SIGTERM")
    }
    await Promise.all([...this.#children].map(async (child) => child.exited))
    await Promise.all([...this.#stages, ...this.#rollbackPaths].map(async (path) => rm(path, { force: true, recursive: true })))
    this.#stages.clear()
    this.#rollbackPaths.clear()
  }

  commit(): void {
    this.#rollbackPaths.clear()
  }
}

async function pauseForInterruptionTest(transaction: Transaction, stage: string): Promise<void> {
  const readyPath = process.env["RELEASE_CANDIDATE_TEST_READY_FILE"]
  if (readyPath === undefined) {
    return
  }
  const child = Bun.spawn({ cmd: ["sh", "-c", "exec tail -f /dev/null"], stderr: "ignore", stdout: "ignore" })
  transaction.addChild(child)
  const childPath = process.env["RELEASE_CANDIDATE_TEST_CHILD_PID_FILE"]
  if (childPath !== undefined) {
    await writeFile(childPath, `${child.pid}\n`)
  }
  await writeFile(readyPath, JSON.stringify({ childPID: child.pid, pid: process.pid, stage }))
  await new Promise<void>(() => {})
}

function exitCode(signal: InterruptSignal): number {
  return signal === "SIGTERM" ? 143 : 130
}

export async function withReleaseTransaction<T>(operation: (transaction: ReleaseTransaction) => Promise<T>): Promise<T> {
  const transaction = new Transaction()
  let interrupted = false
  const interrupt = (signal: InterruptSignal): void => {
    if (interrupted) {
      return
    }
    interrupted = true
    void transaction.cleanup().then(() => process.exit(exitCode(signal)))
  }
  const onInterrupt = (): void => interrupt("SIGINT")
  const onTerminate = (): void => interrupt("SIGTERM")
  process.once("SIGINT", onInterrupt)
  process.once("SIGTERM", onTerminate)
  try {
    const result = await operation(transaction)
    transaction.commit()
    return result
  } finally {
    process.off("SIGINT", onInterrupt)
    process.off("SIGTERM", onTerminate)
    if (!interrupted) {
      await transaction.cleanup()
    }
  }
}

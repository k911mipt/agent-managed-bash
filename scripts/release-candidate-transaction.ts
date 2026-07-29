import { existsSync } from "node:fs"
import { mkdir, mkdtemp, rename, rm, writeFile } from "node:fs/promises"
import { dirname, join } from "node:path"
import { pauseForInterruptionTest } from "./release-candidate-transaction-hooks"
import { ReleaseTransactionState } from "./release-candidate-transaction-state"

type InterruptSignal = "SIGINT" | "SIGTERM"

export type ReleaseTransaction = {
  atomicDirectory: (output: string, prefix: string, write: (stage: string) => Promise<void>) => Promise<void>
  atomicFile: (output: string, prefix: string, content: string) => Promise<void>
}

class Transaction extends ReleaseTransactionState implements ReleaseTransaction {
  async atomicDirectory(output: string, prefix: string, write: (stage: string) => Promise<void>): Promise<void> {
    const stage = await this.#createStage(output, prefix)
    await pauseForInterruptionTest(this, stage)
    const lease = this.beginMutation()
    try {
      await write(stage)
      await rename(stage, output)
      lease.promote(stage)
    } finally {
      lease.release()
    }
  }

  async atomicFile(output: string, prefix: string, content: string): Promise<void> {
    const stage = await this.#createStage(output, prefix)
    const stagedFile = join(stage, "output")
    await pauseForInterruptionTest(this, stage)
    const lease = this.beginMutation()
    try {
      await writeFile(stagedFile, content)
      await rename(stagedFile, output)
      await rm(stage, { force: true, recursive: true })
      lease.promote(stage)
    } finally {
      lease.release()
    }
  }

  async #createStage(output: string, prefix: string): Promise<string> {
    if (existsSync(output)) {
      throw new Error("candidate output already exists")
    }
    const lease = this.beginMutation({ rollbackPath: output })
    try {
      await mkdir(dirname(output), { recursive: true })
      const stage = await mkdtemp(join(dirname(output), prefix))
      lease.ownTemporary(stage)
      return stage
    } finally {
      lease.release()
    }
  }
}

function exitCode(signal: InterruptSignal): number {
  return signal === "SIGTERM" ? 143 : 130
}

type OperationOutcome<T> = { readonly kind: "error"; readonly error: unknown } | { readonly kind: "result"; readonly value: T }

export async function withReleaseTransaction<T>(operation: (transaction: ReleaseTransaction) => Promise<T>): Promise<T> {
  const transaction = new Transaction()
  const signalReady = Promise.withResolvers<InterruptSignal>()
  let signal: InterruptSignal | undefined
  const receiveSignal = (nextSignal: InterruptSignal): void => {
    if (signal !== undefined) {
      return
    }
    signal = nextSignal
    transaction.requestClose()
    signalReady.resolve(nextSignal)
  }
  const onInterrupt = (): void => receiveSignal("SIGINT")
  const onTerminate = (): void => receiveSignal("SIGTERM")
  process.on("SIGINT", onInterrupt)
  process.on("SIGTERM", onTerminate)
  const outcome = operation(transaction).then<OperationOutcome<T>, OperationOutcome<T>>(
    (value) => ({ kind: "result", value }),
    (error: unknown) => ({ error, kind: "error" }),
  )
  let signalled = false
  const exitForSignal = async (receivedSignal: InterruptSignal): Promise<never> => {
    signalled = true
    await transaction.cleanup()
    process.exit(exitCode(receivedSignal))
  }
  try {
    const winner = await Promise.race([outcome, signalReady.promise.then((nextSignal) => ({ kind: "signal" as const, signal: nextSignal }))])
    if (winner.kind === "signal") {
      return exitForSignal(winner.signal)
    }
    if (signal !== undefined) {
      return exitForSignal(signal)
    }
    if (winner.kind === "error") {
      throw winner.error
    }
    transaction.commit()
    return winner.value
  } finally {
    if (!signalled) {
      await transaction.cleanup()
      process.off("SIGINT", onInterrupt)
      process.off("SIGTERM", onTerminate)
    }
  }
}

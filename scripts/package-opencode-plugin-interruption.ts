const interruptionExitCodes = { SIGINT: 130, SIGTERM: 143 } as const
const terminationGraceMilliseconds = 1_000

export type InterruptSignal = keyof typeof interruptionExitCodes

export class PackageInterruptedError extends Error {
  readonly exitCode: number

  constructor(readonly signal: InterruptSignal) {
    super(`pack public plugin interrupted by ${signal}`)
    this.name = "PackageInterruptedError"
    this.exitCode = interruptionExitCodes[signal]
  }
}

export type ActivePack = {
  readonly completed: Promise<void>
  readonly subprocess: ReturnType<typeof Bun.spawn>
}

export type PackInterruption = {
  readonly dispose: () => void
  readonly setActivePack: (pack: ActivePack | undefined) => void
  readonly throwIfInterrupted: () => void
  readonly waitForTermination: () => Promise<void>
}

export function createPackInterruption(): PackInterruption {
  let activePack: ActivePack | undefined
  let signal: InterruptSignal | undefined
  let termination: Promise<void> | undefined
  const interrupt = (nextSignal: InterruptSignal): void => {
    if (signal !== undefined) {
      return
    }
    signal = nextSignal
    if (activePack !== undefined) {
      termination = terminatePackTree(activePack)
    }
  }
  const onInterrupt = (): void => interrupt("SIGINT")
  const onTerminate = (): void => interrupt("SIGTERM")
  process.once("SIGINT", onInterrupt)
  process.once("SIGTERM", onTerminate)

  return {
    dispose: () => {
      process.off("SIGINT", onInterrupt)
      process.off("SIGTERM", onTerminate)
    },
    setActivePack: (pack) => {
      activePack = pack
      if (pack !== undefined && signal !== undefined && termination === undefined) {
        termination = terminatePackTree(pack)
      }
    },
    throwIfInterrupted: () => {
      if (signal !== undefined) {
        throw new PackageInterruptedError(signal)
      }
    },
    waitForTermination: async () => {
      if (termination !== undefined) {
        await termination
      }
    },
  }
}

async function terminatePackTree(pack: ActivePack): Promise<void> {
  signalProcessGroup(pack.subprocess.pid, "SIGTERM")
  const stoppedDuringGrace = await Promise.race([
    pack.completed.then(() => true),
    Bun.sleep(terminationGraceMilliseconds).then(() => false),
  ])
  if (!stoppedDuringGrace) {
    signalProcessGroup(pack.subprocess.pid, "SIGKILL")
  }
}

function signalProcessGroup(processID: number, signal: "SIGTERM" | "SIGKILL"): void {
  try {
    process.kill(-processID, signal)
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ESRCH") {
      return
    }
    throw error
  }
}

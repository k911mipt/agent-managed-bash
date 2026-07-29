import { readFile, watch } from "node:fs/promises"
import { dirname } from "node:path"

export type ReadyState = {
  readonly childPID: number
  readonly pid: number
  readonly stage: string
}

type WaitForReadyOptions = {
  readonly onRead?: () => void
  readonly timeoutMilliseconds?: number
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function isAbortError(error: unknown): boolean {
  return error instanceof Error && (error.name === "AbortError" || ("code" in error && error.code === "ABORT_ERR"))
}

function isMissingFileError(error: unknown): boolean {
  return error instanceof Error && "code" in error && error.code === "ENOENT"
}

export async function waitForReady(path: string, options: WaitForReadyOptions = {}): Promise<ReadyState> {
  const readReady = async (): Promise<ReadyState | undefined> => {
    let content: string
    try {
      content = await readFile(path, "utf8")
    } catch (error) {
      if (isMissingFileError(error)) {
        return undefined
      }
      throw error
    }
    options.onRead?.()
    let value: unknown
    try {
      value = JSON.parse(content)
    } catch (error) {
      if (error instanceof SyntaxError) {
        return undefined
      }
      throw error
    }
    if (isRecord(value)
      && typeof value["childPID"] === "number" && typeof value["pid"] === "number" && typeof value["stage"] === "string") {
      return { childPID: value["childPID"], pid: value["pid"], stage: value["stage"] }
    }
    return undefined
  }
  const controller = new AbortController()
  const deadline = setTimeout(() => controller.abort(), options.timeoutMilliseconds ?? 5_000)
  const events = watch(dirname(path), { signal: controller.signal })
  const iterator = events[Symbol.asyncIterator]()
  let nextEvent = iterator.next().then(() => true)
  try {
    while (!controller.signal.aborted) {
      const ready = await readReady()
      if (ready !== undefined) {
        return ready
      }
      try {
        const eventReceived = await Promise.race([nextEvent, Bun.sleep(50).then(() => false)])
        if (eventReceived) {
          nextEvent = iterator.next().then(() => true)
        }
      } catch (error) {
        if (!isAbortError(error)) {
          throw error
        }
      }
    }
    const final = await readReady()
    if (final !== undefined) {
      return final
    }
    if (await Bun.file(path).exists()) {
      throw new Error("invalid release-candidate readiness marker")
    }
    throw new Error("release-candidate readiness marker timed out")
  } finally {
    clearTimeout(deadline)
    controller.abort()
    try {
      await nextEvent
    } catch (error) {
      if (!isAbortError(error)) {
        throw error
      }
    }
    try {
      await iterator.return?.()
    } catch (error) {
      if (!isAbortError(error)) {
        throw error
      }
    }
  }
}

import { createHash } from "node:crypto"
import { isRecord, parseStrictJSON } from "./release-candidate-data"
import { runReleaseCommand } from "./release-publish-command"

export type PublicationAsset = { readonly name: string; readonly path: string; readonly sha256: string }
export type PublicationCandidate = { readonly assets: readonly PublicationAsset[]; readonly commit: string; readonly repository: string; readonly tag: string; readonly version: string; readonly workflowBlob: string }
export type JSONRecord = Record<string, unknown>
export type ReleaseCommandResult = { readonly exitCode: number; readonly stderr: string; readonly stdout: string }

export class ReleasePublicationError extends Error {
  readonly name = "ReleasePublicationError"
}

export function record(value: unknown, field: string): JSONRecord {
  if (!isRecord(value)) {
    throw new ReleasePublicationError(`invalid ${field}`)
  }
  return value
}

export function string(value: unknown, field: string): string {
  if (typeof value !== "string" || value.length === 0) {
    throw new ReleasePublicationError(`invalid ${field}`)
  }
  return value
}

export function option(arguments_: readonly string[], name: string): string {
  const index = arguments_.indexOf(name)
  const value = index < 0 ? undefined : arguments_[index + 1]
  if (value === undefined || value.startsWith("--")) {
    throw new ReleasePublicationError(`missing ${name}`)
  }
  return value
}

export function environment(name: string): string {
  return string(process.env[name], name)
}

export function expectedSRI(bytes: Uint8Array): string {
  return `sha512-${createHash("sha512").update(bytes).digest("base64")}`
}

function commandTimeout(): number {
  const configured = process.env["RELEASE_COMMAND_TIMEOUT_MS"]
  if (configured === undefined) return 30_000
  const value = Number(configured)
  if (!Number.isSafeInteger(value) || value < 1 || value > 300_000) throw new ReleasePublicationError("invalid release command timeout")
  return value
}

export async function command(executable: string, arguments_: readonly string[], environment_: Readonly<Record<string, string>> = {}): Promise<ReleaseCommandResult> {
  const binary = executable === "gh" ? process.env["RELEASE_GH_BIN"] ?? "gh" : executable === "npm" ? process.env["RELEASE_NPM_BIN"] ?? "npm" : executable === "curl" ? process.env["RELEASE_CURL_BIN"] ?? "curl" : executable
  const environment = Object.fromEntries(Object.entries(process.env).filter((entry): entry is [string, string] => entry[0] !== "NPM_TOKEN" && entry[1] !== undefined))
  const result = await runReleaseCommand({ arguments: arguments_, environment: { ...environment, ...environment_ }, executable: binary, inheritEnvironment: false, timeoutMilliseconds: commandTimeout() })
  if (result.kind === "timed_out") throw new ReleasePublicationError(`release command timed out: ${executable}`)
  return result
}

export async function readCommand(executable: string, arguments_: readonly string[]): Promise<unknown | undefined> {
  const result = await command(executable, arguments_)
  if (result.exitCode === 1 && (result.stderr.includes("E404") || result.stderr.includes("not found"))) {
    return undefined
  }
  if (result.exitCode !== 0) {
    throw new ReleasePublicationError(`read-back command failed: ${executable}`)
  }
  return parseStrictJSON(result.stdout)
}

export async function mutateThenRead(executable: string, arguments_: readonly string[], read: () => Promise<void>, environment_: Readonly<Record<string, string>> = {}): Promise<void> {
  const attempts = Number(process.env["RELEASE_READ_ATTEMPTS"] ?? "3")
  if (!Number.isSafeInteger(attempts) || attempts < 1 || attempts > 10) throw new ReleasePublicationError("invalid release read attempts")
  const reconcile = async (): Promise<void> => {
    let failure: unknown = undefined
    for (let index = 0; index < attempts; index += 1) {
      try {
        await read()
        return
      } catch (error) {
        failure = error
      }
    }
    throw failure
  }
  try {
    const result = await command(executable, arguments_, environment_)
    if (result.exitCode !== 0) throw new ReleasePublicationError(`mutation command failed: ${executable}`)
  } catch (error) {
    try {
      await reconcile()
      return
    } catch {
      throw error
    }
  }
  await reconcile()
}

import { createHash } from "node:crypto"
import { lstat, readFile } from "node:fs/promises"
import { parseStrictJSON } from "./release-candidate-json"

export type JSONRecord = Record<string, unknown>

export const isRecord = (value: unknown): value is JSONRecord => typeof value === "object" && value !== null && !Array.isArray(value)
export const digest = (value: string | Uint8Array): string => createHash("sha256").update(value).digest("hex")

export async function regularBytes(path: string, maximum = Number.MAX_SAFE_INTEGER): Promise<Uint8Array> {
  const info = await lstat(path)
  if (!info.isFile()) {
    throw new Error(`expected regular file: ${path}`)
  }
  if (info.size > maximum) {
    throw new Error(`file exceeds maximum size: ${path}`)
  }
  return readFile(path)
}

export async function readJSON(path: string, maximum = Number.MAX_SAFE_INTEGER): Promise<unknown> {
  return parseStrictJSON(new TextDecoder("utf-8", { fatal: true }).decode(await regularBytes(path, maximum)))
}

export function requireString(value: unknown, field: string): string {
  if (typeof value !== "string") {
    throw new Error(`invalid ${field}`)
  }
  return value
}

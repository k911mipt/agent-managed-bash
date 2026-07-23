import { readFile } from "node:fs/promises"
import { resolve } from "node:path"
import Ajv2020, {
  type JSONSchemaType,
  type ValidateFunction,
} from "ajv/dist/2020"
import type { SchemaRoot } from "./protocol-validators"

export type FixtureCase = {
  readonly name: string
  readonly schema: SchemaRoot
  readonly path: string
  readonly valid: boolean
}

export type FixtureManifest = {
  readonly cases: readonly FixtureCase[]
}

export type ParseResult =
  | { readonly kind: "parsed"; readonly value: unknown }
  | { readonly kind: "rejected" }

const manifestSchema = {
  type: "object",
  additionalProperties: false,
  properties: {
    cases: {
      type: "array",
      minItems: 1,
      items: {
        type: "object",
        additionalProperties: false,
        properties: {
          name: { type: "string", minLength: 1 },
          schema: { type: "string", enum: ["request", "response", "state"] },
          path: { type: "string", minLength: 1 },
          valid: { type: "boolean" },
        },
        required: ["name", "schema", "path", "valid"],
      },
    },
  },
  required: ["cases"],
} as const satisfies JSONSchemaType<FixtureManifest>

export function parseRawJSON(rawDocument: Uint8Array): ParseResult {
  try {
    const text = new TextDecoder("utf-8", { fatal: true }).decode(rawDocument)
    const value: unknown = JSON.parse(text)
    return { kind: "parsed", value }
  } catch (error: unknown) {
    if (error instanceof SyntaxError || error instanceof TypeError) {
      return { kind: "rejected" }
    }
    throw error
  }
}

export async function loadFixtureManifest(repositoryRoot: string): Promise<FixtureManifest> {
  const path = resolve(repositoryRoot, "fixtures/v1/schema/manifest.json")
  const parsed = parseRawJSON(await readFile(path))
  switch (parsed.kind) {
    case "parsed":
      if (!validateManifest(parsed.value)) {
        throw new TypeError(`invalid fixture manifest: ${path}`)
      }
      return parsed.value
    case "rejected":
      throw new TypeError(`invalid fixture manifest: ${path}`)
    default:
      return assertNever(parsed)
  }
}

export function validateRawDocument(
  rawDocument: Uint8Array,
  validator: ValidateFunction<unknown>,
): boolean {
  const parsed = parseRawJSON(rawDocument)
  switch (parsed.kind) {
    case "parsed":
      return validator(parsed.value)
    case "rejected":
      return false
    default:
      return assertNever(parsed)
  }
}

const manifestAjv = new Ajv2020({ strict: true })
const validateManifest = manifestAjv.compile(manifestSchema)

function assertNever(value: never): never {
  throw new TypeError(`unreachable variant: ${String(value)}`)
}

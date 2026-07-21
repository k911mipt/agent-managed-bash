import { readFile } from "node:fs/promises"
import { resolve } from "node:path"
import Ajv2020, {
  type AnySchemaObject,
  type JSONSchemaType,
  type ValidateFunction,
} from "ajv/dist/2020"

export type SchemaRoot = "request" | "response" | "state"

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

export type ProtocolValidators = Readonly<Record<SchemaRoot, ValidateFunction<unknown>>>

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

const schemaFiles = [
  "models.schema.json",
  "request.schema.json",
  "response.schema.json",
  "state.schema.json",
] as const

const schemaBaseURL = "https://agent-managed-bash.dev/schema/v1/"

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

export async function compileProtocolValidators(
  repositoryRoot: string,
): Promise<ProtocolValidators> {
  const ajv = new Ajv2020({ allErrors: true, strict: true })
  for (const fileName of schemaFiles) {
    const path = resolve(repositoryRoot, "schemas/v1", fileName)
    const parsed = parseRawJSON(await readFile(path))
    switch (parsed.kind) {
      case "parsed":
        if (!isSchemaObject(parsed.value)) {
          throw new TypeError(`invalid JSON schema: ${path}`)
        }
        ajv.addSchema(parsed.value)
        break
      case "rejected":
        throw new TypeError(`invalid JSON schema: ${path}`)
      default:
        assertNever(parsed)
    }
  }

  return {
    request: requireValidator(ajv.getSchema(`${schemaBaseURL}request.schema.json`), "request"),
    response: requireValidator(ajv.getSchema(`${schemaBaseURL}response.schema.json`), "response"),
    state: requireValidator(ajv.getSchema(`${schemaBaseURL}state.schema.json`), "state"),
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

function isSchemaObject(value: unknown): value is AnySchemaObject {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function requireValidator(
  validator: ValidateFunction<unknown> | undefined,
  root: SchemaRoot,
): ValidateFunction<unknown> {
  if (validator === undefined) {
    throw new TypeError(`missing compiled ${root} schema`)
  }
  return validator
}

function assertNever(value: never): never {
  throw new TypeError(`unreachable variant: ${String(value)}`)
}

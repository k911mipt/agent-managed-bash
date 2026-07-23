import Ajv2020, { type AnySchemaObject, type ValidateFunction } from "ajv/dist/2020"

export type SchemaRoot = "request" | "response" | "state"

export type ProtocolValidators = Readonly<Record<SchemaRoot, ValidateFunction<unknown>>>

const schemaBaseURL = "https://agent-managed-bash.dev/schema/v1/"

export function compileProtocolValidators(schemas: readonly unknown[]): ProtocolValidators {
  const ajv = new Ajv2020({ allErrors: true, strict: true })
  for (const schema of schemas) {
    if (!isSchemaObject(schema)) {
      throw new TypeError("invalid embedded JSON schema")
    }
    ajv.addSchema(schema)
  }

  return {
    request: requireValidator(ajv.getSchema(`${schemaBaseURL}request.schema.json`), "request"),
    response: requireValidator(ajv.getSchema(`${schemaBaseURL}response.schema.json`), "response"),
    state: requireValidator(ajv.getSchema(`${schemaBaseURL}state.schema.json`), "state"),
  }
}

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

import type { Response } from "./generated/protocol.gen"
import { compileProtocolValidators } from "./schema-fixture-harness"

export type ResponseValidator = (value: unknown) => value is Response

export async function createResponseValidator(repositoryRoot: string): Promise<ResponseValidator> {
  const validators = await compileProtocolValidators(repositoryRoot)

  return (value: unknown): value is Response => validators.response(value)
}

import type { Response } from "./generated/protocol.gen"
import { protocolValidators } from "./protocol-schemas"

export type ResponseValidator = (value: unknown) => value is Response

export function createResponseValidator(): ResponseValidator {
  return (value: unknown): value is Response => protocolValidators.response(value)
}

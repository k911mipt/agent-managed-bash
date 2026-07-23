import modelsSchema from "../../../schemas/v1/models.schema.json"
import requestSchema from "../../../schemas/v1/request.schema.json"
import responseSchema from "../../../schemas/v1/response.schema.json"
import stateSchema from "../../../schemas/v1/state.schema.json"
import { compileProtocolValidators } from "./protocol-validators"

export const protocolValidators = compileProtocolValidators([
  modelsSchema,
  requestSchema,
  responseSchema,
  stateSchema,
])

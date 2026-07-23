import { describe, expect, test } from "bun:test"
import { createResponseValidator } from "./response-validator"

describe("embedded response validation", () => {
  test("loads without a repository root and validates the version contract", async () => {
    // Given
    const validate = await createResponseValidator()
    const validResponse = {
      schema_version: 1,
      ok: true,
      action: "version",
      result: {
        product: "managed-bash",
        binary_version: "0.1.0",
        protocol_version: 1,
        os: "linux",
        architecture: "amd64",
      },
    }

    // When / Then
    expect(validate(validResponse)).toBe(true)
    expect(validate({ ...validResponse, unexpected: true })).toBe(false)
  })
})

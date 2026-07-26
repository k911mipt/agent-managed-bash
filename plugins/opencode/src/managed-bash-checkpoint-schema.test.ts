import { describe, expect, test } from "bun:test"
import { readFile } from "node:fs/promises"
import { resolve } from "node:path"
import Ajv2020 from "ajv/dist/2020"
import fixtureManifest from "../../../fixtures/events/v1/managed-bash-checkpoint/manifest.json"
import checkpointSchema from "../../../schemas/events/v1/managed-bash-checkpoint.schema.json"
import { parseRawJSON } from "./schema-fixture-harness"

const repositoryRoot = resolve(import.meta.dir, "../../..")
const fixtureRoot = resolve(repositoryRoot, "fixtures/events/v1/managed-bash-checkpoint")
const validateCheckpoint = new Ajv2020({ allErrors: true, strict: true }).compile(checkpointSchema)

describe("managed bash checkpoint event schema", () => {
  for (const fixture of fixtureManifest.cases) {
    test(fixture.name, async () => {
      // Given
      const rawFixture = await readFile(resolve(fixtureRoot, fixture.path))

      // When
      const parsed = parseRawJSON(rawFixture)
      const valid = parsed.kind === "parsed" && validateCheckpoint(parsed.value)

      // Then
      expect(valid).toBe(fixture.valid)
    })
  }
})

import { describe, expect, test } from "bun:test"
import { readFile } from "node:fs/promises"
import { resolve } from "node:path"
import {
  loadFixtureManifest,
  validateRawDocument,
} from "./schema-fixture-harness"
import { protocolValidators } from "./protocol-schemas"

const repositoryRoot = resolve(import.meta.dir, "../../..")
const fixtureRoot = resolve(repositoryRoot, "fixtures/v1/schema")
const manifest = await loadFixtureManifest(repositoryRoot)

describe("protocol schema fixtures", () => {
  for (const fixture of manifest.cases) {
    test(fixture.name, async () => {
      // Given
      const rawFixture = await readFile(resolve(fixtureRoot, fixture.path))

      // When
      const valid = validateRawDocument(rawFixture, protocolValidators[fixture.schema])

      // Then
      expect(valid).toBe(fixture.valid)
    })
  }
})

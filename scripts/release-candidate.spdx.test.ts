import { describe, expect, test } from "bun:test"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { tmpdir } from "node:os"
import {
  MAX_SPDX_BYTES,
  canonicalizeJSON,
  parseStrictJSON,
  validateSpdxDocument,
  validateSpdxDocuments,
  validateSpdxFile,
  verifyOfficialSpdxSchema,
} from "./release-candidate"
import { validSpdxDocument } from "./release-candidate-fixtures.test"

const schemaPath = resolve(import.meta.dir, "../schemas/spdx-schema.json")

describe("official SPDX validation", () => {
  test("verifies the pinned official SPDX schema and accepts a semantic SPDX 2.3 inventory", async () => {
    // Given
    const document = validSpdxDocument()

    // When / Then
    await expect(verifyOfficialSpdxSchema(schemaPath)).resolves.toBeUndefined()
    await expect(validateSpdxDocument(document, schemaPath)).resolves.toBeUndefined()
    expect(canonicalizeJSON({ z: [3, { b: true, a: false }], a: "value" })).toBe(
      '{"a":"value","z":[3,{"a":false,"b":true}]}',
    )
  })

  test("rejects lone surrogate JSON strings and a document describing itself", async () => {
    // Given
    const selfDescribing = { ...validSpdxDocument(), documentDescribes: ["SPDXRef-DOCUMENT"] }

    // When / Then
    expect(() => canonicalizeJSON({ [String.fromCharCode(0xd800)]: "valid" })).toThrow("surrogate")
    expect(() => canonicalizeJSON({ valid: String.fromCharCode(0xd800) })).toThrow("surrogate")
    await expect(validateSpdxDocument(selfDescribing, schemaPath)).rejects.toThrow("package or file")
  })

  test("rejects marker-only, empty, and broken SPDX inventory relationships", async () => {
    // Given
    const markerOnly = { ...validSpdxDocument(), packages: [] }
    const emptyInventory = { ...validSpdxDocument(), documentDescribes: [], packages: [] }
    const brokenRelationship = {
      ...validSpdxDocument(),
      relationships: [
        {
          relatedSpdxElement: "SPDXRef-Missing",
          relationshipType: "DESCRIBES",
          spdxElementId: "SPDXRef-DOCUMENT",
        },
      ],
    }

    // When / Then
    await expect(validateSpdxDocument(markerOnly, schemaPath)).rejects.toThrow("empty SPDX inventory")
    await expect(validateSpdxDocument(emptyInventory, schemaPath)).rejects.toThrow("inventory")
    await expect(validateSpdxDocument(brokenRelationship, schemaPath)).rejects.toThrow("relationship")
  })

  test("requires meaningful identity on a described package instead of an undescribed package", async () => {
    // Given
    const describedWeak = {
      ...validSpdxDocument(),
      packages: [
        {
          SPDXID: "SPDXRef-Package",
          downloadLocation: "NOASSERTION",
          name: "",
        },
        {
          SPDXID: "SPDXRef-Undescribed",
          checksums: [{ algorithm: "SHA256", checksumValue: "0".repeat(64) }],
          downloadLocation: "NOASSERTION",
          name: "meaningful-but-undescribed",
          versionInfo: "1.0.0",
        },
      ],
    }
    const emptyDescribed = {
      ...validSpdxDocument(),
      packages: [{ SPDXID: "SPDXRef-Package", downloadLocation: "NOASSERTION", name: "" }],
    }

    // When / Then
    await expect(validateSpdxDocument(describedWeak, schemaPath)).rejects.toThrow("identity")
    await expect(validateSpdxDocument(emptyDescribed, schemaPath)).rejects.toThrow("identity")
  })

  test("accepts a described file-only SPDX inventory with filename and checksum", async () => {
    // Given
    const fileOnly = {
      ...validSpdxDocument(),
      documentDescribes: ["SPDXRef-File"],
      files: [{ SPDXID: "SPDXRef-File", checksums: [{ algorithm: "SHA256", checksumValue: "0".repeat(64) }], fileName: "managed-bash.js" }],
      packages: [],
      relationships: [{ relatedSpdxElement: "SPDXRef-File", relationshipType: "DESCRIBES", spdxElementId: "SPDXRef-DOCUMENT" }],
    }

    // When / Then
    await expect(validateSpdxDocument(fileOnly, schemaPath)).resolves.toBeUndefined()
  })

  test("rejects a duplicate namespace, malformed bytes, and oversized SBOM before assembly", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-spdx-"))
    const malformed = join(root, "malformed.spdx.json")
    const oversized = join(root, "oversized.spdx.json")
    await writeFile(malformed, "{")
    await writeFile(oversized, " ".repeat(MAX_SPDX_BYTES + 1))

    try {
      // When / Then
      await expect(validateSpdxDocuments([validSpdxDocument(), validSpdxDocument()], schemaPath)).rejects.toThrow("namespace")
      await expect(validateSpdxFile(malformed, schemaPath)).rejects.toThrow("JSON")
      await expect(validateSpdxFile(oversized, schemaPath)).rejects.toThrow("maximum")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("rejects duplicate raw JSON keys at every object depth and accepts valid nested JSON", () => {
    // Given
    const duplicateRoot = '{"key":1,"key":2}'
    const duplicateNested = '{"outer":{"key":1,"\\u006bey":2}}'
    const validNested = '{"outer":{"key":[true,null,-1.25e+2]}}'

    // When / Then
    expect(() => parseStrictJSON(duplicateRoot)).toThrow("duplicate")
    expect(() => parseStrictJSON(duplicateNested)).toThrow("duplicate")
    expect(parseStrictJSON(validNested)).toEqual({ outer: { key: [true, null, -125] } })
  })

  test("rejects duplicate raw SPDX keys before schema validation", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-duplicate-json-"))
    const duplicate = join(root, "duplicate.spdx.json")
    const document = JSON.stringify(validSpdxDocument())
    await writeFile(duplicate, document.replace('"name":"agent-managed-bash-0.1.0-linux-amd64.tar.gz"', '"name":"agent-managed-bash-0.1.0-linux-amd64.tar.gz","name":"duplicate"'))

    try {
      // When / Then
      await expect(validateSpdxFile(duplicate, schemaPath)).rejects.toThrow("duplicate")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })
})

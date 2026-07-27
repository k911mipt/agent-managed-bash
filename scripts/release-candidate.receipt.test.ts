import { describe, expect, test } from "bun:test"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import {
  createProducerReceipt,
  createRelationReceipt,
  validateProducerReceipt,
  verifyProducerReceipt,
  verifyRelationReceipt,
} from "./release-candidate"
import { fixtureCommit, fixtureVersion, validSpdxDocument } from "./release-candidate-fixtures.test"
import { resolve } from "node:path"

const schemaPath = resolve(import.meta.dir, "../schemas/spdx-schema.json")

describe("release candidate receipts", () => {
  test("creates and verifies a canonical producer receipt for exact artifact bytes", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-receipt-"))
    const asset = join(root, "asset.tgz")
    await writeFile(asset, "primary bytes\n")

    try {
      // When
      const receipt = await createProducerReceipt({
        assetPath: asset,
        commit: fixtureCommit,
        name: "asset.tgz",
        version: fixtureVersion,
      })

      // Then
      expect(receipt).toEqual({
        commit: fixtureCommit,
        name: "asset.tgz",
        sha256: "756c9578dd8d3f006add35f3ff5855834a5ddd959210eed018e7da018dd5bea2",
        size: 14,
        version: fixtureVersion,
      })
      await expect(verifyProducerReceipt({ assetPath: asset, receipt })).resolves.toBeUndefined()
      await writeFile(asset, "mutated bytes\n")
      await expect(verifyProducerReceipt({ assetPath: asset, receipt })).rejects.toThrow("sha256")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("rejects malformed and extra producer receipt fields", () => {
    // Given
    const malformed = {
      commit: fixtureCommit,
      extra: true,
      name: "asset.tgz",
      sha256: "0".repeat(64),
      size: 1,
      version: fixtureVersion,
    }

    // When / Then
    expect(() => validateProducerReceipt(malformed)).toThrow("unexpected")
  })

  test("binds a relation receipt to canonical SPDX content and rejects swapped SBOM bytes", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-relation-"))
    const asset = join(root, "asset.tgz")
    const predicate = join(root, "asset.tgz.spdx.json")
    await writeFile(asset, "primary bytes\n")
    await writeFile(predicate, `${JSON.stringify(validSpdxDocument("asset.tgz"))}\n`)

    try {
      const subject = await createProducerReceipt({
        assetPath: asset,
        commit: fixtureCommit,
        name: "asset.tgz",
        version: fixtureVersion,
      })

      // When
      const relation = await createRelationReceipt({ assetPath: asset, predicatePath: predicate, schemaPath, subject })

      // Then
      await expect(verifyRelationReceipt({ assetPath: asset, predicatePath: predicate, relation, schemaPath, subject })).resolves.toBeUndefined()
      await writeFile(predicate, `${JSON.stringify(validSpdxDocument("other.tgz"))}\n`)
      await expect(verifyRelationReceipt({ assetPath: asset, predicatePath: predicate, relation, schemaPath, subject })).rejects.toThrow("identity")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })
})

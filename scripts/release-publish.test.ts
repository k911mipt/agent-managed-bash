import { describe, expect, test } from "bun:test"
import { createHash } from "node:crypto"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { releasePublication } from "./release-publish"

describe("release publication", () => {
  test("stages only the candidate tarball and leaves a complete draft when npm is absent", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-publish-"))
    const tarballBytes = "candidate bytes\n"
    const assets = await Promise.all(Array.from({ length: 12 }, async (_, index) => {
      const name = `candidate-${index}`
      const path = join(root, name)
      await writeFile(path, tarballBytes)
      return { name, path, sha256: createHash("sha256").update(tarballBytes).digest("hex") }
    }))

    try {
      // When
      const result = await releasePublication({
        candidate: {
          assets,
          commit: "0123456789abcdef0123456789abcdef01234567",
          repository: "k911mipt/agent-managed-bash",
          tag: "v0.1.0",
          version: "0.1.0",
          workflowBlob: "0123456789abcdef0123456789abcdef01234567",
        },
        mode: "stage",
      })

      // Then
      expect(result.kind).toBe("staged")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })
})

import { describe, expect, test } from "bun:test"
import { createHash } from "node:crypto"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { releasePublication } from "./release-publish"
import { fakeStatePath, runPublication, setupPublication } from "./release-publish-cli-fixtures"

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

  test("fails closed when a non-404 attestation error contains not found", async () => {
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-api-error-"))
    const environment = await setupPublication(root)
    await writeFile(fakeStatePath(environment), JSON.stringify({ attestationAPIError: "repository not found after authentication failure" }))

    try {
      const result = await runPublication(["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)

      expect(result.exitCode).not.toBe(0)
      expect(result.stderr).toContain("attestation enumeration failed")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })
})

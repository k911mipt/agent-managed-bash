import { describe, expect, test } from "bun:test"
import { mkdtemp, readdir, rm, writeFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { tmpdir } from "node:os"
import {
  assembleReleaseCandidate,
  createCandidateControl,
  createCandidateMetadata,
  validateCandidateControl,
  validateReleaseCandidate,
} from "./release-candidate"
import {
  fixtureCommit,
  fixtureVersion,
  primaryAssetNames,
  scannedAssetNames,
  writeCandidateFixture,
} from "./release-candidate-fixtures.test"

const schemaPath = resolve(import.meta.dir, "../schemas/spdx-schema.json")

describe("release candidate assembly", () => {
  test("assembles exactly six public primary assets, five SPDX assets, and SHA256SUMS", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-candidate-"))
    const output = join(root, "candidate")
    await writeCandidateFixture(root)

    try {
      // When
      await assembleReleaseCandidate({
        outputDirectory: output,
        producerDirectory: join(root, "producers"),
        relationDirectory: join(root, "relations"),
        schemaPath,
      })

      // Then
      expect((await readdir(output)).sort()).toEqual(
        [...primaryAssetNames, ...scannedAssetNames.map((name) => `${name}.spdx.json`), "SHA256SUMS"].sort(),
      )
      await expect(validateReleaseCandidate(output)).resolves.toBeUndefined()
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("binds a separate candidate control receipt to immutable artifact identity and inputs", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-control-"))
    const output = join(root, "candidate")
    const controlPath = join(root, "control", "CANDIDATE-RECEIPT.json")
    const metadataPath = join(root, "candidate-metadata.json")
    await writeCandidateFixture(root)
    await assembleReleaseCandidate({ outputDirectory: output, producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), schemaPath })
    await createCandidateMetadata({ candidateArtifact: { digest: "a".repeat(64), id: "123" }, candidateDirectory: output, metadataPath, producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), schemaPath, trustedContext: { commit: fixtureCommit, repository: "k911mipt/agent-managed-bash", runAttempt: "1", runId: "456", tag: `v${fixtureVersion}`, version: fixtureVersion, workflow: "release.yml", workflowBlob: fixtureCommit } })

    try {
      // When
      await createCandidateControl({
        candidateDirectory: output,
        candidateArtifact: { digest: "a".repeat(64), id: "123" },
        commit: fixtureCommit,
        controlPath,
        producerDirectory: join(root, "producers"),
        relationDirectory: join(root, "relations"),
        repository: "k911mipt/agent-managed-bash",
        metadataPath,
        run: { attempt: "1", id: "456", workflow: "release.yml" },
        schemaPath,
        tag: `v${fixtureVersion}`,
        trustedContext: { commit: fixtureCommit, repository: "k911mipt/agent-managed-bash", runAttempt: "1", runId: "456", tag: `v${fixtureVersion}`, version: fixtureVersion, workflow: "release.yml", workflowBlob: fixtureCommit },
        version: fixtureVersion,
      })

      // Then
      await expect(validateCandidateControl({ candidateDirectory: output, controlPath, expectedArtifact: { digest: "a".repeat(64), id: "123" } })).resolves.toBeUndefined()
      await expect(validateCandidateControl({ candidateDirectory: output, controlPath, expectedArtifact: { digest: "b".repeat(64), id: "123" } })).rejects.toThrow("artifact")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("rejects duplicate producer roles, extra candidate output, and control context mismatch", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-hostile-"))
    const output = join(root, "candidate")
    const controlPath = join(root, "control", "CANDIDATE-RECEIPT.json")
    const metadataPath = join(root, "candidate-metadata.json")
    await writeCandidateFixture(root)
    await writeFile(join(root, "producers", "install-release.sh.receipt.json"), await Bun.file(join(root, "producers", "k911mipt-opencode-agent-managed-bash-0.1.0.tgz.receipt.json")).text())
    try {
      // When / Then
      await expect(assembleReleaseCandidate({ outputDirectory: output, producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), schemaPath })).rejects.toThrow("identity")
      await writeCandidateFixture(root)
      await assembleReleaseCandidate({ outputDirectory: output, producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), schemaPath })
      await createCandidateMetadata({ candidateArtifact: { digest: "a".repeat(64), id: "123" }, candidateDirectory: output, metadataPath, producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), schemaPath, trustedContext: { commit: fixtureCommit, repository: "k911mipt/agent-managed-bash", runAttempt: "1", runId: "456", tag: `v${fixtureVersion}`, version: fixtureVersion, workflow: "release.yml", workflowBlob: fixtureCommit } })
      await writeFile(join(output, "extra"), "extra")
      await expect(validateReleaseCandidate(output)).rejects.toThrow("unexpected")
      await rm(join(output, "extra"))
      await createCandidateControl({ candidateDirectory: output, candidateArtifact: { digest: "a".repeat(64), id: "123" }, commit: fixtureCommit, controlPath, metadataPath, producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), repository: "k911mipt/agent-managed-bash", run: { attempt: "1", id: "456", workflow: "release.yml" }, schemaPath, tag: `v${fixtureVersion}`, trustedContext: { commit: fixtureCommit, repository: "k911mipt/agent-managed-bash", runAttempt: "1", runId: "456", tag: `v${fixtureVersion}`, version: fixtureVersion, workflow: "release.yml", workflowBlob: fixtureCommit }, version: fixtureVersion })
      await expect(createCandidateControl({ candidateDirectory: output, candidateArtifact: { digest: "a".repeat(64), id: "123" }, commit: fixtureCommit, controlPath, metadataPath, producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), repository: "wrong/repository", run: { attempt: "1", id: "456", workflow: "release.yml" }, schemaPath, tag: `v${fixtureVersion}`, trustedContext: { commit: fixtureCommit, repository: "k911mipt/agent-managed-bash", runAttempt: "1", runId: "456", tag: `v${fixtureVersion}`, version: fixtureVersion, workflow: "release.yml", workflowBlob: fixtureCommit }, version: fixtureVersion })).rejects.toThrow("repository")
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  const rejectionCases = [
    { name: "mutated producer", mutate: async (root: string): Promise<void> => writeFile(join(root, "producers", primaryAssetNames[0]), "swapped") },
    { name: "swapped SBOM", mutate: async (root: string): Promise<void> => writeFile(join(root, "relations", `${scannedAssetNames[0]}.spdx.json`), "{}") },
    { name: "extra input", mutate: async (root: string): Promise<void> => writeFile(join(root, "producers", "unexpected"), "unexpected") },
    { name: "missing relation", mutate: async (root: string): Promise<void> => rm(join(root, "relations", `${scannedAssetNames[0]}.spdx.json`)) },
  ] as const
  for (const testCase of rejectionCases) {
    test(`rejects ${testCase.name} without leaving a candidate output`, async () => {
        // Given
        const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-reject-"))
        const output = join(root, "candidate")
        await writeCandidateFixture(root)
        await testCase.mutate(root)

        try {
          // When / Then
          await expect(assembleReleaseCandidate({ outputDirectory: output, producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), schemaPath })).rejects.toThrow()
          await expect(Bun.file(output).exists()).resolves.toBeFalse()
        } finally {
          await rm(root, { force: true, recursive: true })
        }
    })
  }
})

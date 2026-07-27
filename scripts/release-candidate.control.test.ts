import { describe, expect, test } from "bun:test"
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { tmpdir } from "node:os"
import {
  assembleReleaseCandidate,
  createCandidateMetadata,
  validateCandidateControl,
} from "./release-candidate"
import {
  fixtureCommit,
  fixtureVersion,
  writeCandidateFixture,
} from "./release-candidate-fixtures.test"

const schemaPath = resolve(import.meta.dir, "../schemas/spdx-schema.json")
const scriptPath = resolve(import.meta.dir, "release-candidate.ts")
const candidateArtifact = { digest: "a".repeat(64), id: "123" }
const trustedEnvironment = {
  RELEASE_COMMIT: fixtureCommit,
  RELEASE_REPOSITORY: "k911mipt/agent-managed-bash",
  RELEASE_RUN_ATTEMPT: "1",
  RELEASE_RUN_ID: "456",
  RELEASE_TAG: `v${fixtureVersion}`,
  RELEASE_VERSION: fixtureVersion,
  RELEASE_WORKFLOW: "release.yml",
  RELEASE_WORKFLOW_BLOB: fixtureCommit,
} as const

type CommandResult = {
  readonly exitCode: number
  readonly stderr: string
  readonly stdout: string
}

async function createControlInputs(root: string): Promise<void> {
  const candidate = join(root, "candidate")
  await writeCandidateFixture(root)
  await assembleReleaseCandidate({
    outputDirectory: candidate,
    producerDirectory: join(root, "producers"),
    relationDirectory: join(root, "relations"),
    schemaPath,
  })
  await createCandidateMetadata({
    candidateArtifact,
    candidateDirectory: candidate,
    metadataPath: join(root, "candidate-metadata.json"),
    producerDirectory: join(root, "producers"),
    relationDirectory: join(root, "relations"),
    schemaPath,
    trustedContext: {
      commit: fixtureCommit,
      repository: "k911mipt/agent-managed-bash",
      runAttempt: "1",
      runId: "456",
      tag: `v${fixtureVersion}`,
      version: fixtureVersion,
      workflow: "release.yml",
      workflowBlob: fixtureCommit,
    },
  })
}

async function runControl(
  root: string,
  environment: Readonly<Record<string, string>> = {},
  artifact: { readonly digest: string; readonly id: string } = candidateArtifact,
): Promise<CommandResult> {
  const childProcess = Bun.spawn({
    cmd: [
      process.execPath,
      scriptPath,
      "control",
      "--candidate",
      join(root, "candidate"),
      "--artifact-id",
      artifact.id,
      "--artifact-digest",
      artifact.digest,
      "--metadata",
      join(root, "candidate-metadata.json"),
      "--output",
      join(root, "control", "CANDIDATE-RECEIPT.json"),
      "--producers",
      join(root, "producers"),
      "--relations",
      join(root, "relations"),
      "--schema",
      schemaPath,
    ],
    env: { ...process.env, ...trustedEnvironment, ...environment },
    stderr: "pipe",
    stdout: "pipe",
  })
  const [exitCode, stderr, stdout] = await Promise.all([
    childProcess.exited,
    new Response(childProcess.stderr).text(),
    new Response(childProcess.stdout).text(),
  ])
  return { exitCode, stderr, stdout }
}

async function replaceMetadata(root: string, expression: RegExp, replacement: string): Promise<void> {
  const path = join(root, "candidate-metadata.json")
  const before = await readFile(path, "utf8")
  const after = before.replace(expression, replacement)
  expect(after).not.toBe(before)
  await writeFile(path, after)
}

async function reorderMetadataArray(root: string, key: "producers" | "relations"): Promise<void> {
  const path = join(root, "candidate-metadata.json")
  const before = await readFile(path, "utf8")
  const prefix = `"${key}":[`
  const start = before.indexOf(prefix)
  const end = before.indexOf("]", start)
  expect(start).toBeGreaterThanOrEqual(0)
  expect(end).toBeGreaterThan(start)
  const items = before.slice(start + prefix.length, end).split("},{")
  expect(items.length).toBeGreaterThan(1)
  const [first, second] = items
  if (first === undefined || second === undefined) {
    throw new Error("fixture metadata has too few receipts")
  }
  items[0] = second
  items[1] = first
  await writeFile(path, `${before.slice(0, start + prefix.length)}${items.join("},{")}${before.slice(end)}`)
}

describe("release candidate control identity", () => {
  test("creates and fully validates a control receipt from exact private metadata", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-control-"))
    await createControlInputs(root)

    try {
      // When
      const result = await runControl(root)

      // Then
      expect(result.exitCode).toBe(0)
      expect(result.stdout).toBe("")
      await expect(validateCandidateControl({
        candidateDirectory: join(root, "candidate"),
        controlPath: join(root, "control", "CANDIDATE-RECEIPT.json"),
        expectedArtifact: candidateArtifact,
      })).resolves.toBeUndefined()
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("never overwrites a pre-existing control output", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-control-existing-"))
    const controlPath = join(root, "control", "CANDIDATE-RECEIPT.json")
    await createControlInputs(root)
    await Bun.write(controlPath, "sentinel\n")

    try {
      // When
      const result = await runControl(root)

      // Then
      expect(result.exitCode).not.toBe(0)
      expect(result.stdout).toBe("")
      expect(await Bun.file(controlPath).text()).toBe("sentinel\n")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  const contextMismatches = [
    { environment: { RELEASE_REPOSITORY: "other/repository" }, name: "repository" },
    { environment: { RELEASE_WORKFLOW: "other.yml" }, name: "workflow" },
    { environment: { RELEASE_RUN_ID: "457" }, name: "run ID" },
    { environment: { RELEASE_RUN_ATTEMPT: "2" }, name: "run attempt" },
    { environment: { RELEASE_TAG: "v0.1.1" }, name: "tag" },
    { environment: { RELEASE_COMMIT: "fedcba9876543210fedcba9876543210fedcba98" }, name: "commit" },
    { environment: { RELEASE_VERSION: "0.1.1" }, name: "version" },
  ] as const
  for (const testCase of contextMismatches) {
    test(`rejects a wrong trusted ${testCase.name} without control output`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-control-context-"))
      await createControlInputs(root)

      try {
        // When
        const result = await runControl(root, testCase.environment)

        // Then
        expect(result.exitCode).not.toBe(0)
        expect(result.stdout).toBe("")
        await expect(Bun.file(join(root, "control", "CANDIDATE-RECEIPT.json")).exists()).resolves.toBeFalse()
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    })
  }

  const artifactMismatches = [
    { artifact: { ...candidateArtifact, id: "124" }, name: "artifact ID" },
    { artifact: { ...candidateArtifact, digest: "b".repeat(64) }, name: "artifact digest" },
  ] as const
  for (const testCase of artifactMismatches) {
    test(`rejects a wrong candidate ${testCase.name} without control output`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-control-artifact-"))
      await createControlInputs(root)

      try {
        // When
        const result = await runControl(root, {}, testCase.artifact)

        // Then
        expect(result.exitCode).not.toBe(0)
        expect(result.stdout).toBe("")
        await expect(Bun.file(join(root, "control", "CANDIDATE-RECEIPT.json")).exists()).resolves.toBeFalse()
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    })
  }

  const metadataMismatches = [
    {
      mutate: (root: string) => replaceMetadata(root, /"manifest":"[^"]+/, '"manifest":"wrong"'),
      name: "candidate manifest",
    },
    {
      mutate: (root: string) => replaceMetadata(root, /"manifest_sha256":"[a-f0-9]{64}/, `"manifest_sha256":"${"b".repeat(64)}`),
      name: "candidate manifest digest",
    },
    {
      mutate: (root: string) => replaceMetadata(root, /"producers":\[\{"commit":"[a-f0-9]{40}/, '"producers":[{"commit":"fedcba9876543210fedcba9876543210fedcba98'),
      name: "producer receipt",
    },
    {
      mutate: (root: string) => reorderMetadataArray(root, "producers"),
      name: "producer receipt order",
    },
    {
      mutate: (root: string) => replaceMetadata(root, /"relations":\[\{"predicate_media_type":"application\/spdx\+json","predicate_name":"[^"]+","predicate_sha256":"[a-f0-9]{64}/, '"relations":[{"predicate_media_type":"application/spdx+json","predicate_name":"substituted.spdx.json","predicate_sha256":"0000000000000000000000000000000000000000000000000000000000000000'),
      name: "relation receipt",
    },
    {
      mutate: (root: string) => reorderMetadataArray(root, "relations"),
      name: "relation receipt order",
    },
  ] as const
  for (const testCase of metadataMismatches) {
    test(`rejects a ${testCase.name} mismatch without control output`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-control-metadata-"))
      await createControlInputs(root)
      await testCase.mutate(root)

      try {
        // When
        const result = await runControl(root)

        // Then
        expect(result.exitCode).not.toBe(0)
        expect(result.stdout).toBe("")
        await expect(Bun.file(join(root, "control", "CANDIDATE-RECEIPT.json")).exists()).resolves.toBeFalse()
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    })
  }
})

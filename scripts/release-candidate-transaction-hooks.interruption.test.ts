import { describe, expect, test } from "bun:test"
import { mkdtemp, readdir, rm } from "node:fs/promises"
import { basename, dirname, join, resolve } from "node:path"
import { tmpdir } from "node:os"
import { writeCandidateFixture } from "./release-candidate-fixtures.test"
import { waitForReady } from "./release-candidate-readiness.test-helper"

const schemaPath = resolve(import.meta.dir, "../schemas/spdx-schema.json")
const scriptPath = resolve(import.meta.dir, "release-candidate.ts")
const environment = {
  RELEASE_COMMIT: "0123456789abcdef0123456789abcdef01234567",
  RELEASE_REPOSITORY: "k911mipt/agent-managed-bash",
  RELEASE_RUN_ATTEMPT: "1",
  RELEASE_RUN_ID: "456",
  RELEASE_TAG: "v0.1.0",
  RELEASE_VERSION: "0.1.0",
  RELEASE_WORKFLOW: "release.yml",
  RELEASE_WORKFLOW_BLOB: "0123456789abcdef0123456789abcdef01234567",
} as const

type ReadinessHooks = {
  readonly continueAfterReady?: boolean
  readonly readyPath: string
  readonly renamedPath?: string
}

async function runMetadata(root: string, hooks: ReadinessHooks): Promise<ReturnType<typeof Bun.spawn>> {
  const assembly = Bun.spawn({
    cmd: [process.execPath, scriptPath, "assemble", "--producers", join(root, "producers"), "--relations", join(root, "relations"), "--output", join(root, "candidate"), "--schema", schemaPath],
    cwd: root,
    env: { ...process.env, ...environment }, stderr: "pipe", stdout: "pipe",
  })
  expect(await assembly.exited).toBe(0)
  return Bun.spawn({
    cmd: [process.execPath, scriptPath, "metadata", "--candidate", join(root, "candidate"), "--artifact-id", "123", "--artifact-digest", "a".repeat(64), "--producers", join(root, "producers"), "--relations", join(root, "relations"), "--schema", schemaPath, "--output", join(root, "candidate-metadata.json")],
    cwd: root,
    env: {
      ...process.env,
      ...environment,
      RELEASE_CANDIDATE_TEST_READY_FILE: hooks.readyPath,
      ...(hooks.continueAfterReady ? { RELEASE_CANDIDATE_TEST_READY_CONTINUE: "1" } : {}),
      ...(hooks.renamedPath === undefined ? {} : { RELEASE_CANDIDATE_TEST_READY_RENAMED_FILE: hooks.renamedPath }),
    },
    stderr: "pipe", stdout: "pipe",
  })
}

async function assertTerminated(processID: number): Promise<void> {
  try {
    process.kill(processID, 0)
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ESRCH") {
      return
    }
    throw error
  }
  throw new Error(`process ${processID} remains alive`)
}

async function assertNoReadyMarkerStages(path: string): Promise<void> {
  const prefix = `.${basename(path)}-`
  expect((await readdir(dirname(path))).some((entry) => entry.startsWith(prefix) && entry.endsWith(".tmp"))).toBeFalse()
}

async function assertDirectoryExists(path: string): Promise<void> {
  await expect(readdir(path)).resolves.toEqual(expect.any(Array))
}

describe("release candidate readiness publication ownership", () => {
  for (const signal of ["SIGTERM", "SIGINT"] as const) {
    test(`rolls back a readiness marker renamed before ${signal}`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-ready-renamed-"))
      const readyPath = join(root, "metadata-ready.json")
      const renamedPath = join(root, "metadata-renamed.json")
      await writeCandidateFixture(root)
      const child = await runMetadata(root, { readyPath, renamedPath })
      const renamed = await waitForReady(renamedPath)

      try {
        // When
        child.kill(signal)
        const exitCode = await child.exited

        // Then
        expect(exitCode).toBe(signal === "SIGTERM" ? 143 : 130)
        await expect(Bun.file(readyPath).exists()).resolves.toBeFalse()
        await assertNoReadyMarkerStages(readyPath)
        await expect(Bun.file(join(root, "candidate-metadata.json")).exists()).resolves.toBeFalse()
        await assertDirectoryExists(join(root, "candidate"))
        await assertTerminated(renamed.pid)
        await assertTerminated(renamed.childPID)
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    })
  }

  for (const signal of ["SIGTERM", "SIGINT"] as const) {
    test(`rolls back a published readiness marker on ${signal}`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-ready-published-"))
      const readyPath = join(root, "metadata-ready.json")
      await writeCandidateFixture(root)
      const child = await runMetadata(root, { readyPath })
      const ready = await waitForReady(readyPath)

      try {
        // When
        child.kill(signal)
        const exitCode = await child.exited

        // Then
        expect(exitCode).toBe(signal === "SIGTERM" ? 143 : 130)
        await expect(Bun.file(readyPath).exists()).resolves.toBeFalse()
        await assertNoReadyMarkerStages(readyPath)
        await expect(Bun.file(join(root, "candidate-metadata.json")).exists()).resolves.toBeFalse()
        await assertTerminated(ready.pid)
        await assertTerminated(ready.childPID)
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    })
  }

  test("preserves a published readiness marker after commit", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-ready-commit-"))
    const readyPath = join(root, "metadata-ready.json")
    await writeCandidateFixture(root)
    const child = await runMetadata(root, { continueAfterReady: true, readyPath })

    try {
      // When
      expect(await child.exited).toBe(0)

      // Then
      await expect(Bun.file(readyPath).exists()).resolves.toBeTrue()
      await assertNoReadyMarkerStages(readyPath)
      await expect(Bun.file(join(root, "candidate-metadata.json")).exists()).resolves.toBeTrue()
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })
})

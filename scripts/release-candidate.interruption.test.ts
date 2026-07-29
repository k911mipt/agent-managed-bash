import { describe, expect, test } from "bun:test"
import { mkdtemp, readdir, rm, writeFile } from "node:fs/promises"
import { basename, dirname, join, resolve } from "node:path"
import { tmpdir } from "node:os"
import { writeCandidateFixture } from "./release-candidate-fixtures.test"
import { waitForReady } from "./release-candidate-readiness.test-helper"
import "./release-candidate-readiness.interruption.test"
import "./release-candidate-transaction-state.test"
import "./release-candidate-transaction-hooks.interruption.test"

const schemaPath = resolve(import.meta.dir, "../schemas/spdx-schema.json")
const scriptPath = resolve(import.meta.dir, "release-candidate.ts")
const interruptionTestTimeoutMilliseconds = 20_000
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

type ReadinessPaths = {
  readonly readyPath?: string
  readonly stagedReadyPath?: string
}

async function stdoutText(childProcess: ReturnType<typeof Bun.spawn>): Promise<string> {
  if (typeof childProcess.stdout === "number" || childProcess.stdout === undefined) {
    throw new Error("release-candidate subprocess stdout is not piped")
  }
  return new Response(childProcess.stdout).text()
}

async function runCommand(arguments_: readonly string[], root: string, readiness?: ReadinessPaths): Promise<ReturnType<typeof Bun.spawn>> {
  return Bun.spawn({
    cmd: [process.execPath, scriptPath, ...arguments_],
    env: {
      ...process.env,
      ...environment,
      ...(readiness?.readyPath === undefined ? {} : {
        RELEASE_CANDIDATE_TEST_CHILD_PID_FILE: `${readiness.readyPath}.child`,
        RELEASE_CANDIDATE_TEST_READY_FILE: readiness.readyPath,
      }),
      ...(readiness?.stagedReadyPath === undefined ? {} : {
        RELEASE_CANDIDATE_TEST_READY_STAGE_FILE: readiness.stagedReadyPath,
      }),
    },
    stderr: "pipe",
    stdout: "pipe",
    cwd: root,
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

async function assertDirectoryExists(path: string): Promise<void> {
  await expect(readdir(path)).resolves.toEqual(expect.any(Array))
}

async function assertNoReadyMarkerStages(path: string): Promise<void> {
  const prefix = `.${basename(path)}-`
  expect((await readdir(dirname(path))).some((entry) => entry.startsWith(prefix) && entry.endsWith(".tmp"))).toBeFalse()
}

describe("release candidate interruption cleanup", () => {
  for (const signal of ["SIGTERM", "SIGINT"] as const) {
    test(`cleans every assembly stage and reaps children on ${signal}`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-assemble-interrupt-"))
      const readyPath = join(root, "assemble-ready.json")
      await writeCandidateFixture(root)
      const childProcess = await runCommand([
        "assemble", "--producers", join(root, "producers"), "--relations", join(root, "relations"),
        "--output", join(root, "candidate"), "--schema", schemaPath,
      ], root, { readyPath })
      const ready = await waitForReady(readyPath)
      await assertNoReadyMarkerStages(readyPath)

      try {
        // When
        childProcess.kill(signal)
        const [exitCode, stdout] = await Promise.all([childProcess.exited, stdoutText(childProcess)])

        // Then
        expect(exitCode).toBe(signal === "SIGTERM" ? 143 : 130)
        expect(stdout).toBe("")
        await expect(Bun.file(ready.stage).exists()).resolves.toBeFalse()
        await expect(readdir(root)).resolves.not.toContain("candidate")
        await assertTerminated(ready.pid)
        await assertTerminated(ready.childPID)
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    }, interruptionTestTimeoutMilliseconds)
  }

  for (const signal of ["SIGTERM", "SIGINT"] as const) {
    test(`cleans metadata stages and preserves the candidate on ${signal}`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-metadata-interrupt-"))
      const readyPath = join(root, "metadata-ready.json")
      await writeCandidateFixture(root)
      const assembly = await runCommand([
        "assemble", "--producers", join(root, "producers"), "--relations", join(root, "relations"),
        "--output", join(root, "candidate"), "--schema", schemaPath,
      ], root)
      expect(await assembly.exited).toBe(0)
      const childProcess = await runCommand([
        "metadata", "--candidate", join(root, "candidate"), "--artifact-id", "123", "--artifact-digest", "a".repeat(64),
        "--producers", join(root, "producers"), "--relations", join(root, "relations"), "--schema", schemaPath,
        "--output", join(root, "candidate-metadata.json"),
      ], root, { readyPath })
      const ready = await waitForReady(readyPath)
      await assertNoReadyMarkerStages(readyPath)

      try {
        // When
        childProcess.kill(signal)
        const [exitCode, stdout] = await Promise.all([childProcess.exited, stdoutText(childProcess)])

        // Then
        expect(exitCode).toBe(signal === "SIGTERM" ? 143 : 130)
        expect(stdout).toBe("")
        await expect(Bun.file(ready.stage).exists()).resolves.toBeFalse()
        await expect(Bun.file(join(root, "candidate-metadata.json")).exists()).resolves.toBeFalse()
        await assertDirectoryExists(join(root, "candidate"))
        await assertTerminated(ready.pid)
        await assertTerminated(ready.childPID)
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    }, interruptionTestTimeoutMilliseconds)
  }

  for (const signal of ["SIGTERM", "SIGINT"] as const) {
    test(`cleans control stages and preserves candidate inputs on ${signal}`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-control-interrupt-"))
      const readyPath = join(root, "control-ready.json")
      await writeCandidateFixture(root)
      const assembly = await runCommand([
        "assemble", "--producers", join(root, "producers"), "--relations", join(root, "relations"),
        "--output", join(root, "candidate"), "--schema", schemaPath,
      ], root)
      expect(await assembly.exited).toBe(0)
      const metadata = await runCommand([
        "metadata", "--candidate", join(root, "candidate"), "--artifact-id", "123", "--artifact-digest", "a".repeat(64),
        "--producers", join(root, "producers"), "--relations", join(root, "relations"), "--schema", schemaPath,
        "--output", join(root, "candidate-metadata.json"),
      ], root)
      expect(await metadata.exited).toBe(0)
      const childProcess = await runCommand([
        "control", "--candidate", join(root, "candidate"), "--artifact-id", "123", "--artifact-digest", "a".repeat(64),
        "--metadata", join(root, "candidate-metadata.json"), "--output", join(root, "control", "CANDIDATE-RECEIPT.json"),
        "--producers", join(root, "producers"), "--relations", join(root, "relations"), "--schema", schemaPath,
      ], root, { readyPath })
      const ready = await waitForReady(readyPath)
      await assertNoReadyMarkerStages(readyPath)

      try {
        // When
        childProcess.kill(signal)
        const [exitCode, stdout] = await Promise.all([childProcess.exited, stdoutText(childProcess)])

        // Then
        expect(exitCode).toBe(signal === "SIGTERM" ? 143 : 130)
        expect(stdout).toBe("")
        await expect(Bun.file(ready.stage).exists()).resolves.toBeFalse()
        await expect(Bun.file(join(root, "control", "CANDIDATE-RECEIPT.json")).exists()).resolves.toBeFalse()
        await assertDirectoryExists(join(root, "candidate"))
        await expect(Bun.file(join(root, "candidate-metadata.json")).exists()).resolves.toBeTrue()
        await assertTerminated(ready.pid)
        await assertTerminated(ready.childPID)
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    }, interruptionTestTimeoutMilliseconds)
  }

  for (const signal of ["SIGTERM", "SIGINT"] as const) {
    test(`removes readiness staging before rename on ${signal}`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-ready-stage-interrupt-"))
      const readyPath = join(root, "metadata-ready.json")
      const stagedReadyPath = join(root, "metadata-before-rename.json")
      await writeCandidateFixture(root)
      const assembly = await runCommand([
        "assemble", "--producers", join(root, "producers"), "--relations", join(root, "relations"),
        "--output", join(root, "candidate"), "--schema", schemaPath,
      ], root)
      expect(await assembly.exited).toBe(0)
      const childProcess = await runCommand([
        "metadata", "--candidate", join(root, "candidate"), "--artifact-id", "123", "--artifact-digest", "a".repeat(64),
        "--producers", join(root, "producers"), "--relations", join(root, "relations"), "--schema", schemaPath,
        "--output", join(root, "candidate-metadata.json"),
      ], root, { readyPath, stagedReadyPath })
      const staged = await waitForReady(stagedReadyPath)

      try {
        // When
        childProcess.kill(signal)
        const [exitCode, stdout] = await Promise.all([childProcess.exited, stdoutText(childProcess)])

        // Then
        expect(exitCode).toBe(signal === "SIGTERM" ? 143 : 130)
        expect(stdout).toBe("")
        await expect(Bun.file(readyPath).exists()).resolves.toBeFalse()
        await assertNoReadyMarkerStages(readyPath)
        await expect(Bun.file(staged.stage).exists()).resolves.toBeFalse()
        await expect(Bun.file(join(root, "candidate-metadata.json")).exists()).resolves.toBeFalse()
        await assertDirectoryExists(join(root, "candidate"))
        await assertTerminated(staged.pid)
        await assertTerminated(staged.childPID)
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    }, interruptionTestTimeoutMilliseconds)
  }
})

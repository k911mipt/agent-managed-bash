import { describe, expect, test } from "bun:test"
import { mkdtemp, readFile, readdir, rm, watch } from "node:fs/promises"
import { basename, dirname, join, resolve } from "node:path"
import { tmpdir } from "node:os"
import { writeCandidateFixture } from "./release-candidate-fixtures.test"

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

type ReadyState = {
  readonly childPID: number
  readonly pid: number
  readonly stage: string
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

async function waitForReady(path: string): Promise<ReadyState> {
  const readReady = async (): Promise<ReadyState> => {
    const value: unknown = JSON.parse(await readFile(path, "utf8"))
    if (isRecord(value)
      && typeof value["childPID"] === "number" && typeof value["pid"] === "number" && typeof value["stage"] === "string") {
      return { childPID: value["childPID"], pid: value["pid"], stage: value["stage"] }
    }
    throw new Error("invalid release-candidate readiness marker")
  }
  if (await Bun.file(path).exists()) {
    return readReady()
  }
  const events = watch(dirname(path), { signal: AbortSignal.timeout(5_000) })
  for await (const event of events) {
    if (event.filename === basename(path) && await Bun.file(path).exists()) {
      return readReady()
    }
  }
  throw new Error("release-candidate readiness marker timed out")
}

async function stdoutText(childProcess: ReturnType<typeof Bun.spawn>): Promise<string> {
  if (typeof childProcess.stdout === "number" || childProcess.stdout === undefined) {
    throw new Error("release-candidate subprocess stdout is not piped")
  }
  return new Response(childProcess.stdout).text()
}

async function runCommand(arguments_: readonly string[], root: string, readyPath?: string): Promise<ReturnType<typeof Bun.spawn>> {
  return Bun.spawn({
    cmd: [process.execPath, scriptPath, ...arguments_],
    env: {
      ...process.env,
      ...environment,
      ...(readyPath === undefined ? {} : {
        RELEASE_CANDIDATE_TEST_CHILD_PID_FILE: `${readyPath}.child`,
        RELEASE_CANDIDATE_TEST_READY_FILE: readyPath,
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
      ], root, readyPath)
      const ready = await waitForReady(readyPath)

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
    })
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
      ], root, readyPath)
      const ready = await waitForReady(readyPath)

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
    })
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
      ], root, readyPath)
      const ready = await waitForReady(readyPath)

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
    })
  }
})

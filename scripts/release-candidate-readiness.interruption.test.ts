import { describe, expect, test } from "bun:test"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { waitForReady } from "./release-candidate-readiness.test-helper"

describe("release candidate readiness waiter", () => {
  test("waits for readiness marker completion after a partial first read", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-ready-partial-"))
    const readyPath = join(root, "ready.json")
    const expectedReady = { childPID: 123, pid: 456, stage: join(root, ".candidate-test") }
    const partialRead = Promise.withResolvers<void>()
    await writeFile(readyPath, '{"childPID":123')
    const waiter = waitForReady(readyPath, { onRead: () => partialRead.resolve() }).then(
      (value) => ({ kind: "ready" as const, value }),
      (error: unknown) => ({ error, kind: "error" as const }),
    )

    try {
      // When
      await partialRead.promise
      await Promise.resolve()
      await writeFile(readyPath, JSON.stringify(expectedReady))

      // Then
      await expect(waiter).resolves.toEqual({ kind: "ready", value: expectedReady })
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("reports a final invalid readiness marker at its deadline", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-ready-invalid-"))
    const readyPath = join(root, "ready.json")
    await writeFile(readyPath, JSON.stringify({ childPID: 123 }))

    try {
      // When / Then
      await expect(waitForReady(readyPath, { timeoutMilliseconds: 5 })).rejects.toThrow("invalid release-candidate readiness marker")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })
})

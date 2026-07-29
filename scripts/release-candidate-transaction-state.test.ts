import { describe, expect, test } from "bun:test"
import { mkdtemp, rename, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { ReleaseTransactionState } from "./release-candidate-transaction-state"

describe("release candidate transaction state", () => {
  test("closes admission before cleanup waits for an admitted lease", async () => {
    // Given
    const transaction = new ReleaseTransactionState()
    const lease = transaction.beginMutation()

    // When
    expect(transaction.requestClose()).toBeTrue()
    const cleanup = transaction.cleanup()

    // Then
    expect(() => transaction.beginMutation()).toThrow("release transaction is closing")
    lease.release()
    await cleanup
    expect(transaction.state).toBe("closed")
  })

  test("removes a temp created after cleanup begins by an admitted lease", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-transaction-deferred-"))
    const temporaryPath = join(root, "temporary")
    const transaction = new ReleaseTransactionState()
    const lease = transaction.beginMutation()
    lease.ownTemporary(temporaryPath)

    try {
      // When
      const cleanup = transaction.cleanup()
      await writeFile(temporaryPath, "late")
      lease.release()
      await cleanup

      // Then
      await expect(Bun.file(temporaryPath).exists()).resolves.toBeFalse()
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("removes a final path promoted after closure", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-transaction-promote-"))
    const temporaryPath = join(root, "temporary")
    const finalPath = join(root, "final")
    const transaction = new ReleaseTransactionState()
    const lease = transaction.beginMutation({ rollbackPath: finalPath, temporaryPath })
    await writeFile(temporaryPath, "published")

    try {
      // When
      const cleanup = transaction.cleanup()
      await rename(temporaryPath, finalPath)
      lease.promote(temporaryPath)
      lease.release()
      await cleanup

      // Then
      await expect(Bun.file(finalPath).exists()).resolves.toBeFalse()
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("preserves a committed final path", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-transaction-commit-"))
    const finalPath = join(root, "final")
    const transaction = new ReleaseTransactionState()
    const lease = transaction.beginMutation({ rollbackPath: finalPath })
    await writeFile(finalPath, "committed")
    lease.release()

    try {
      // When
      transaction.commit()
      await transaction.cleanup()

      // Then
      await expect(Bun.file(finalPath).exists()).resolves.toBeTrue()
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("shares one cleanup across repeated close requests", async () => {
    // Given
    const transaction = new ReleaseTransactionState()
    const lease = transaction.beginMutation()

    // When
    expect(transaction.requestClose()).toBeTrue()
    const firstCleanup = transaction.cleanup()
    let settled = false
    void firstCleanup.then(() => {
      settled = true
    })
    await Promise.resolve()
    expect(transaction.requestClose()).toBeFalse()
    const secondCleanup = transaction.cleanup()

    // Then
    expect(settled).toBeFalse()
    lease.release()
    await Promise.all([firstCleanup, secondCleanup])
    expect(transaction.state).toBe("closed")
  })
})

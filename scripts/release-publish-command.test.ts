import { expect, test } from "bun:test"
import { mkdtemp, readFile, rm } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { runReleaseCommand } from "./release-publish-command"
import {
  createTermIgnoringPipeHolder,
  killProcessGroup,
  waitForStoppedProcess,
  waitForTextFile,
} from "./release-publish-command-fixtures"

test("reaps a TERM-ignoring pipe-holding descendant when command times out", async () => {
  // Given
  const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-command-"))
  const fixture = await createTermIgnoringPipeHolder(root)
  const executable = Bun.which("bun")
  if (executable === null) throw new Error("Bun executable is unavailable")

  try {
    // When
    const result = await runReleaseCommand({ arguments: [fixture.script], environment: { CHILD_READY_MARKER: fixture.childReadyMarker, MARKER: fixture.childMarker }, executable, timeoutMilliseconds: 100 })

    // Then
    expect(result.kind).toBe("timed_out")
    expect(result.stdout).toBe("pipe-holder-ready\n")
    const pid = Number(await readFile(fixture.childMarker, "utf8"))
    await waitForStoppedProcess(pid, 500)
  } finally {
    await rm(root, { force: true, recursive: true })
  }
})

test("rejects a live TERM-ignoring descendant before its process group is killed", async () => {
  // Given
  const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-command-"))
  const fixture = await createTermIgnoringPipeHolder(root)
  const executable = Bun.which("bun")
  if (executable === null) throw new Error("Bun executable is unavailable")
  const parent = Bun.spawn([executable, fixture.script], {
    detached: true,
    env: { ...process.env, CHILD_READY_MARKER: fixture.childReadyMarker, MARKER: fixture.childMarker },
    stderr: "ignore",
    stdout: "ignore",
  })

  try {
    const pid = Number(await waitForTextFile(fixture.childMarker, 500))
    await waitForTextFile(fixture.childReadyMarker, 500)
    killProcessGroup(parent.pid, "SIGTERM")

    // When
    const stopped = waitForStoppedProcess(pid, 20)

    // Then
    await expect(stopped).rejects.toThrow(`process ${pid} remained running`)
  } finally {
    killProcessGroup(parent.pid, "SIGKILL")
    await parent.exited
    await rm(root, { force: true, recursive: true })
  }
})

test("treats absent and zombie process states as stopped", async () => {
  // Given
  const absent = () => undefined
  const zombie = () => "Z+"

  // When
  const absentResult = waitForStoppedProcess(1, 20, absent)
  const zombieResult = waitForStoppedProcess(1, 20, zombie)

  // Then
  await expect(absentResult).resolves.toBeUndefined()
  await expect(zombieResult).resolves.toBeUndefined()
})

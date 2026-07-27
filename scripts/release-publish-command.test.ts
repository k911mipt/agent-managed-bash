import { expect, test } from "bun:test"
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { runReleaseCommand } from "./release-publish-command"

test("reaps a TERM-ignoring pipe-holding descendant when command times out", async () => {
  // Given
  const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-command-"))
  const marker = join(root, "child.pid")
  const script = join(root, "hang.ts")
  await writeFile(script, `const child = Bun.spawn({ cmd: [Bun.which("bun")!, "-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)"], stderr: "inherit", stdout: "inherit" }); await Bun.write(process.env.MARKER!, String(child.pid)); await new Promise(() => {})`)

  try {
    // When
    const result = await runReleaseCommand({ arguments: [script], environment: { MARKER: marker }, executable: Bun.which("bun")!, timeoutMilliseconds: 100 })

    // Then
    expect(result.kind).toBe("timed_out")
    const pid = Number(await readFile(marker, "utf8"))
    expect(() => process.kill(pid, 0)).toThrow()
  } finally {
    await rm(root, { force: true, recursive: true })
  }
})

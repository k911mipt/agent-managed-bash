import { randomUUID } from "node:crypto"
import { rename, rm, writeFile } from "node:fs/promises"
import { basename, dirname, join } from "node:path"
import { ReleaseTransactionState } from "./release-candidate-transaction-state"

type ReadyState = { readonly childPID: number; readonly pid: number; readonly stage: string }

function readyState(child: ReturnType<typeof Bun.spawn>, stage: string): ReadyState {
  return { childPID: child.pid, pid: process.pid, stage }
}

export async function pauseForInterruptionTest(transaction: ReleaseTransactionState, stage: string): Promise<void> {
  const readyPath = process.env["RELEASE_CANDIDATE_TEST_READY_FILE"]
  if (readyPath === undefined) {
    return
  }
  const stagedReadyPath = join(dirname(readyPath), `.${basename(readyPath)}-${randomUUID()}.tmp`)
  const lease = transaction.beginMutation({ rollbackPath: readyPath, temporaryPath: stagedReadyPath })
  try {
    const child = Bun.spawn({ cmd: ["sh", "-c", "exec tail -f /dev/null"], stderr: "ignore", stdout: "ignore" })
    transaction.registerChild(child)
    const state = readyState(child, stage)
    const childPath = process.env["RELEASE_CANDIDATE_TEST_CHILD_PID_FILE"]
    if (childPath !== undefined) {
      await writeFile(childPath, `${child.pid}\n`)
    }
    await writeFile(stagedReadyPath, JSON.stringify(state), { flag: "wx" })
    const stagedHookPath = process.env["RELEASE_CANDIDATE_TEST_READY_STAGE_FILE"]
    if (stagedHookPath !== undefined) {
      await writeFile(stagedHookPath, JSON.stringify(state))
      await transaction.waitForClosing()
    }
    await rename(stagedReadyPath, readyPath)
    lease.promote(stagedReadyPath)
    const renamedHookPath = process.env["RELEASE_CANDIDATE_TEST_READY_RENAMED_FILE"]
    if (renamedHookPath !== undefined) {
      await writeFile(renamedHookPath, JSON.stringify(state))
      await transaction.waitForClosing()
    }
  } finally {
    await rm(stagedReadyPath, { force: true })
    lease.release()
  }
  if (process.env["RELEASE_CANDIDATE_TEST_READY_CONTINUE"] === "1") {
    return
  }
  await transaction.waitForClosing()
}

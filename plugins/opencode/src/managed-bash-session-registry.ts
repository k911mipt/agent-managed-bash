import type { TrustedContext } from "./generated/protocol.gen"

export type TrackedJob = {
  readonly jobID: string
  readonly context: TrustedContext
}

type SessionState = {
  readonly jobs: Map<string, TrackedJob>
  readonly pendingLaunches: Set<PendingLaunch>
  closing: boolean
}

export type PendingLaunch = {
  readonly completion: Promise<void>
  readonly signal: AbortSignal
  abort(): boolean
  finish(): void
  track(job: TrackedJob): void
}

type CancelJob = (job: TrackedJob) => Promise<void>

export type SessionRegistry = {
  begin(sessionID: string, abortable: boolean): PendingLaunch | undefined
  close(sessionID: string, cancelJob: CancelJob): Promise<void>
  dispose(cancelJob: CancelJob): Promise<void>
}

export function createSessionRegistry(): SessionRegistry {
  const closedSessionIDs = new Set<string>()
  const deferredCleanups = new Set<Promise<void>>()
  const sessions = new Map<string, SessionState>()
  let disposed = false

  async function close(sessionID: string, cancelJob: CancelJob): Promise<void> {
    closedSessionIDs.add(sessionID)
    const state = sessions.get(sessionID)
    if (state === undefined) {
      return
    }
    state.closing = true
    const pendingLaunches = [...state.pendingLaunches]
    const abortedCompletions: Promise<void>[] = []
    for (const pendingLaunch of pendingLaunches) {
      if (pendingLaunch.abort()) {
        abortedCompletions.push(pendingLaunch.completion)
      } else {
        const cleanup = pendingLaunch.completion.then(() => cancelTrackedJobs(state, cancelJob))
        deferredCleanups.add(cleanup)
        void cleanup.then(() => deferredCleanups.delete(cleanup))
      }
    }
    await Promise.allSettled(abortedCompletions)
    await cancelTrackedJobs(state, cancelJob)
    sessions.delete(sessionID)
  }

  function begin(sessionID: string, abortable: boolean): PendingLaunch | undefined {
    if (disposed || closedSessionIDs.has(sessionID)) {
      return undefined
    }
    const state = sessions.get(sessionID) ?? {
      jobs: new Map<string, TrackedJob>(),
      pendingLaunches: new Set<PendingLaunch>(),
      closing: false,
    }
    sessions.set(sessionID, state)
    if (state.closing) {
      return undefined
    }

    let resolve: (() => void) | undefined
    const completion = new Promise<void>((complete) => {
      resolve = complete
    })
    const controller = new AbortController()
    const pendingLaunch: PendingLaunch = {
      completion,
      signal: controller.signal,
      abort() {
        if (!abortable) {
          return false
        }
        controller.abort()
        return true
      },
      finish() {
        state.pendingLaunches.delete(pendingLaunch)
        resolve?.()
      },
      track(job) {
        state.jobs.set(job.jobID, job)
      },
    }
    state.pendingLaunches.add(pendingLaunch)
    return pendingLaunch
  }

  return {
    begin,
    close,
    async dispose(cancelJob) {
      disposed = true
      await Promise.allSettled([...sessions.keys()].map((sessionID) => close(sessionID, cancelJob)))
      await Promise.allSettled([...deferredCleanups])
    },
  }
}

async function cancelTrackedJobs(state: SessionState, cancelJob: CancelJob): Promise<void> {
  const jobs = [...state.jobs.values()]
  state.jobs.clear()
  await Promise.allSettled(jobs.map(cancelJob))
}

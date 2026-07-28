import { rm } from "node:fs/promises"

export type TransactionState = "open" | "closing" | "committed" | "closed"

export type MutationPaths = {
  readonly rollbackPath?: string
  readonly temporaryPath?: string
}

export type MutationLease = {
  ownRollback: (path: string) => void
  ownTemporary: (path: string) => void
  promote: (temporaryPath: string) => void
  release: () => void
}

export class ReleaseTransactionState {
  readonly #children = new Set<ReturnType<typeof Bun.spawn>>()
  readonly #closing = Promise.withResolvers<void>()
  readonly #active = new Set<Promise<void>>()
  readonly #rollbackPaths = new Set<string>()
  readonly #temporaryPaths = new Set<string>()
  #cleanup: Promise<void> | undefined
  #state: TransactionState = "open"

  get state(): TransactionState {
    return this.#state
  }

  beginMutation(paths: MutationPaths = {}): MutationLease {
    if (this.#state !== "open") {
      throw new Error("release transaction is closing")
    }
    if (paths.rollbackPath !== undefined) {
      this.#rollbackPaths.add(paths.rollbackPath)
    }
    if (paths.temporaryPath !== undefined) {
      this.#temporaryPaths.add(paths.temporaryPath)
    }
    const settled = Promise.withResolvers<void>()
    this.#active.add(settled.promise)
    let released = false
    return {
      ownRollback: (path) => this.#rollbackPaths.add(path),
      ownTemporary: (path) => this.#temporaryPaths.add(path),
      promote: (path) => this.#temporaryPaths.delete(path),
      release: () => {
        if (released) {
          return
        }
        released = true
        this.#active.delete(settled.promise)
        settled.resolve()
      },
    }
  }

  registerChild(child: ReturnType<typeof Bun.spawn>): void {
    this.#children.add(child)
  }

  requestClose(): boolean {
    if (this.#state !== "open") {
      return false
    }
    this.#state = "closing"
    this.#closing.resolve()
    return true
  }

  waitForClosing(): Promise<void> {
    return this.#closing.promise
  }

  commit(): void {
    if (this.#state !== "open") {
      throw new Error("release transaction is closing")
    }
    this.#rollbackPaths.clear()
    this.#state = "committed"
  }

  cleanup(): Promise<void> {
    if (this.#cleanup !== undefined) {
      return this.#cleanup
    }
    const rollback = this.#state !== "committed"
    this.requestClose()
    this.#cleanup = this.#clean(rollback)
    return this.#cleanup
  }

  async #clean(rollback: boolean): Promise<void> {
    this.#terminateChildren()
    await Promise.all([...this.#active])
    this.#terminateChildren()
    await Promise.all([...this.#children].map(async (child) => child.exited))
    const paths = rollback ? [...this.#temporaryPaths, ...this.#rollbackPaths] : [...this.#temporaryPaths]
    await Promise.all(paths.map(async (path) => rm(path, { force: true, recursive: true })))
    this.#temporaryPaths.clear()
    this.#rollbackPaths.clear()
    this.#state = "closed"
  }

  #terminateChildren(): void {
    for (const child of this.#children) {
      child.kill("SIGTERM")
    }
  }
}

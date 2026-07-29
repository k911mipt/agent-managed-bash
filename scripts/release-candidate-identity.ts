import type { TrustedReleaseContext } from "./release-candidate-data"

const commitPattern = /^[a-f0-9]{40}$/
const hashPattern = /^[a-f0-9]{64}$/
const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/

export function assertTrustedContext(context: TrustedReleaseContext): void {
  if (context.repository !== "k911mipt/agent-managed-bash" || context.workflow !== "release.yml" || !/^[1-9]\d*$/.test(context.runId) || !/^[1-9]\d*$/.test(context.runAttempt) || !versionPattern.test(context.version) || context.tag !== `v${context.version}` || !commitPattern.test(context.commit) || !commitPattern.test(context.workflowBlob)) {
    throw new Error("invalid trusted release context")
  }
}

export function isHash(value: string): boolean {
  return hashPattern.test(value)
}

export function isPositiveID(value: string): boolean {
  return /^[1-9]\d*$/.test(value)
}

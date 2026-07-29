import {
  createCandidateControl,
  createCandidateMetadata,
  verifyCandidateControl,
} from "./release-candidate-control"
import { assembleReleaseCandidate } from "./release-candidate-operations"
import {
  createProducerReceipt,
  createRelationReceipt,
  canonicalizeJSON,
  readJSON,
  validateProducerReceipt,
  verifyProducerReceipt,
} from "./release-candidate-data"
import type { TrustedReleaseContext } from "./release-candidate-data"

export * from "./release-candidate-data"
export * from "./release-candidate-control"
export * from "./release-candidate-manifest"
export * from "./release-candidate-operations"

function option(arguments_: readonly string[], name: string): string {
  const index = arguments_.indexOf(name)
  const value = index < 0 ? undefined : arguments_[index + 1]
  if (value === undefined || value.startsWith("--")) {
    throw new Error(`missing ${name}`)
  }
  return value
}

async function writeCanonical(path: string, value: unknown): Promise<void> {
  await Bun.write(path, `${canonicalizeJSON(value)}\n`)
}

function trustedContext(): TrustedReleaseContext {
  const required = (name: string): string => {
    const value = process.env[name]
    if (typeof value !== "string") {
      throw new Error(`invalid ${name}`)
    }
    return value
  }
  return {
    commit: required("RELEASE_COMMIT"),
    repository: required("RELEASE_REPOSITORY"),
    runAttempt: required("RELEASE_RUN_ATTEMPT"),
    runId: required("RELEASE_RUN_ID"),
    tag: required("RELEASE_TAG"),
    version: required("RELEASE_VERSION"),
    workflow: required("RELEASE_WORKFLOW"),
    workflowBlob: required("RELEASE_WORKFLOW_BLOB"),
  }
}

export async function main(arguments_: readonly string[]): Promise<void> {
  const command = arguments_[0]
  if (command === "producer") {
    const receipt = await createProducerReceipt({
      assetPath: option(arguments_, "--asset"),
      commit: option(arguments_, "--commit"),
      name: option(arguments_, "--name"),
      version: option(arguments_, "--version"),
    })
    await writeCanonical(option(arguments_, "--output"), receipt)
    return
  }
  if (command === "verify-producer") {
    const directory = option(arguments_, "--directory")
    const name = option(arguments_, "--name")
    const receipt = validateProducerReceipt(await readJSON(`${directory}/${name}.receipt.json`))
    await verifyProducerReceipt({ assetPath: `${directory}/${name}`, receipt })
    return
  }
  if (command === "relation") {
    const subject = validateProducerReceipt(await readJSON(option(arguments_, "--subject-receipt")))
    const receipt = await createRelationReceipt({
      assetPath: option(arguments_, "--asset"),
      predicatePath: option(arguments_, "--predicate"),
      schemaPath: option(arguments_, "--schema"),
      subject,
    })
    await writeCanonical(option(arguments_, "--output"), receipt)
    return
  }
  if (command === "assemble") {
    await assembleReleaseCandidate({
      outputDirectory: option(arguments_, "--output"),
      producerDirectory: option(arguments_, "--producers"),
      relationDirectory: option(arguments_, "--relations"),
      schemaPath: option(arguments_, "--schema"),
    })
    return
  }
  if (command === "metadata") {
    await createCandidateMetadata({
      candidateArtifact: { digest: option(arguments_, "--artifact-digest"), id: option(arguments_, "--artifact-id") },
      candidateDirectory: option(arguments_, "--candidate"),
      metadataPath: option(arguments_, "--output"),
      producerDirectory: option(arguments_, "--producers"),
      relationDirectory: option(arguments_, "--relations"),
      schemaPath: option(arguments_, "--schema"),
      trustedContext: trustedContext(),
    })
    return
  }
  const context = trustedContext()
  const controlRequest = {
    candidateArtifact: { digest: option(arguments_, "--artifact-digest"), id: option(arguments_, "--artifact-id") },
    candidateDirectory: option(arguments_, "--candidate"),
    commit: context.commit,
    controlPath: option(arguments_, "--output"),
    metadataPath: option(arguments_, "--metadata"),
    producerDirectory: option(arguments_, "--producers"),
    relationDirectory: option(arguments_, "--relations"),
    repository: context.repository,
    run: { attempt: context.runAttempt, id: context.runId, workflow: context.workflow },
    schemaPath: option(arguments_, "--schema"),
    tag: context.tag,
    trustedContext: context,
    version: context.version,
  }
  if (command === "control") {
    await createCandidateControl(controlRequest)
    return
  }
  if (command === "verify-control") {
    await verifyCandidateControl(controlRequest)
    return
  }
  throw new Error("expected producer, verify-producer, relation, assemble, metadata, control, or verify-control command")
}

if (import.meta.main) {
  await main(Bun.argv.slice(2))
}

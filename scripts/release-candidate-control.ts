import {
  assertTrustedContext,
  canonicalizeJSON,
  digest,
  exactKeys,
  isHash,
  isPositiveID,
  isRecord,
  primaryNames,
  readJSON,
  requireString,
  scannedNames,
  validateProducerReceipt,
  validateRelationReceipt,
} from "./release-candidate-data"
import type { ProducerReceipt, RelationReceipt, TrustedReleaseContext } from "./release-candidate-data"
import { readCandidateInputs } from "./release-candidate-inputs"
import type { CandidateInputs } from "./release-candidate-inputs"
import { candidateManifest, validateReleaseCandidate } from "./release-candidate-manifest"
import { withReleaseTransaction } from "./release-candidate-transaction"

export type CandidateArtifact = { readonly digest: string; readonly id: string }

export type CandidateMetadata = {
  readonly candidate: { readonly manifest: string; readonly manifest_sha256: string }
  readonly candidate_artifact: CandidateArtifact
  readonly context: TrustedReleaseContext
  readonly producers: readonly ProducerReceipt[]
  readonly relations: readonly RelationReceipt[]
}

export type CandidateControlRequest = {
  readonly candidateArtifact: CandidateArtifact
  readonly candidateDirectory: string
  readonly commit: string
  readonly controlPath: string
  readonly metadataPath: string
  readonly producerDirectory: string
  readonly relationDirectory: string
  readonly repository: string
  readonly run: { readonly attempt: string; readonly id: string; readonly workflow: string }
  readonly schemaPath: string
  readonly tag: string
  readonly trustedContext: TrustedReleaseContext
  readonly version: string
}

export type CandidateControl = {
  readonly candidate_artifact: CandidateArtifact
  readonly candidate_manifest: { readonly manifest: string; readonly manifest_sha256: string }
  readonly commit: string
  readonly producer_receipts: readonly ProducerReceipt[]
  readonly relations: readonly RelationReceipt[]
  readonly repository: string
  readonly run: { readonly attempt: string; readonly id: string; readonly workflow: string }
  readonly tag: string
  readonly version: string
  readonly workflow_blob: string
}

function identical(left: unknown, right: unknown, field: string): void {
  if (canonicalizeJSON(left) !== canonicalizeJSON(right)) {
    throw new Error(`control ${field} mismatch`)
  }
}

function candidateControl(request: CandidateControlRequest, metadata: CandidateMetadata, inputs: CandidateInputs, manifest: { readonly manifest: string; readonly manifest_sha256: string }): CandidateControl {
  assertTrustedContext(request.trustedContext)
  assertTrustedContext(metadata.context)
  if (!isHash(request.candidateArtifact.digest) || !isPositiveID(request.candidateArtifact.id)) {
    throw new Error("invalid control artifact identity")
  }
  identical(request.repository, request.trustedContext.repository, "trusted repository")
  identical(request.run.workflow, request.trustedContext.workflow, "trusted workflow")
  identical(request.run.id, request.trustedContext.runId, "trusted run ID")
  identical(request.run.attempt, request.trustedContext.runAttempt, "trusted run attempt")
  identical(request.tag, request.trustedContext.tag, "trusted tag")
  identical(request.commit, request.trustedContext.commit, "trusted commit")
  identical(request.version, request.trustedContext.version, "trusted version")
  identical(request.trustedContext, metadata.context, "metadata context")
  identical(manifest, metadata.candidate, "candidate manifest")
  identical(request.candidateArtifact, metadata.candidate_artifact, "candidate artifact")
  identical(inputs.producers, metadata.producers, "producer receipts")
  identical(inputs.relations, metadata.relations, "relation receipts")
  if (inputs.version !== request.version) {
    throw new Error("control candidate version mismatch")
  }
  return { candidate_artifact: request.candidateArtifact, candidate_manifest: manifest, commit: request.commit, producer_receipts: inputs.producers, relations: inputs.relations, repository: request.repository, run: request.run, tag: request.tag, version: request.version, workflow_blob: request.trustedContext.workflowBlob }
}

function contextFromRecord(value: unknown): TrustedReleaseContext {
  if (!isRecord(value)) {
    throw new Error("invalid candidate metadata context")
  }
  exactKeys(value, ["repository", "workflow", "workflowBlob", "runId", "runAttempt", "tag", "commit", "version"])
  return { commit: requireString(value["commit"], "metadata commit"), repository: requireString(value["repository"], "metadata repository"), runAttempt: requireString(value["runAttempt"], "metadata run attempt"), runId: requireString(value["runId"], "metadata run ID"), tag: requireString(value["tag"], "metadata tag"), version: requireString(value["version"], "metadata version"), workflow: requireString(value["workflow"], "metadata workflow"), workflowBlob: requireString(value["workflowBlob"], "metadata workflow blob") }
}

function receiptArray(value: unknown, field: string): readonly unknown[] {
  if (!Array.isArray(value)) {
    throw new Error(`invalid candidate metadata ${field}`)
  }
  return value
}

function validateMetadata(value: unknown): CandidateMetadata {
  if (!isRecord(value)) {
    throw new Error("invalid candidate metadata")
  }
  exactKeys(value, ["context", "candidate", "candidate_artifact", "producers", "relations"])
  const context = contextFromRecord(value["context"])
  assertTrustedContext(context)
  const candidate = value["candidate"]
  const artifact = value["candidate_artifact"]
  if (!isRecord(candidate) || !isRecord(artifact)) {
    throw new Error("invalid candidate metadata identity")
  }
  exactKeys(candidate, ["manifest", "manifest_sha256"])
  exactKeys(artifact, ["id", "digest"])
  const manifest = requireString(candidate["manifest"], "metadata manifest")
  const manifestSha = requireString(candidate["manifest_sha256"], "metadata manifest sha256")
  const candidateArtifact = { digest: requireString(artifact["digest"], "metadata artifact digest"), id: requireString(artifact["id"], "metadata artifact ID") }
  if (!isHash(manifestSha) || digest(manifest) !== manifestSha || !isHash(candidateArtifact.digest) || !isPositiveID(candidateArtifact.id)) {
    throw new Error("invalid candidate metadata identity")
  }
  const producerNames = primaryNames(context.version)
  const producers = receiptArray(value["producers"], "producers").map(validateProducerReceipt)
  if (producers.length !== producerNames.length || producers.some((receipt, index) => receipt.name !== producerNames[index] || receipt.version !== context.version || receipt.commit !== context.commit)) {
    throw new Error("invalid candidate metadata producers")
  }
  const relationNames = scannedNames(context.version).map((name) => `${name}.spdx.json`)
  const relations = receiptArray(value["relations"], "relations").map(validateRelationReceipt)
  if (relations.length !== relationNames.length || relations.some((receipt, index) => receipt.predicate_name !== relationNames[index] || receipt.subject_name !== producerNames[index])) {
    throw new Error("invalid candidate metadata relations")
  }
  return { candidate: { manifest, manifest_sha256: manifestSha }, candidate_artifact: candidateArtifact, context, producers, relations }
}

export async function createCandidateMetadata(request: { readonly candidateArtifact: CandidateArtifact; readonly candidateDirectory: string; readonly metadataPath: string; readonly producerDirectory: string; readonly relationDirectory: string; readonly schemaPath: string; readonly trustedContext: TrustedReleaseContext }): Promise<void> {
  await withReleaseTransaction(async (transaction) => {
    assertTrustedContext(request.trustedContext)
    await validateReleaseCandidate(request.candidateDirectory)
    const inputs = await readCandidateInputs(request.producerDirectory, request.relationDirectory, request.schemaPath)
    if (inputs.version !== request.trustedContext.version || inputs.producers[0]?.commit !== request.trustedContext.commit || !isHash(request.candidateArtifact.digest) || !isPositiveID(request.candidateArtifact.id)) {
      throw new Error("invalid candidate metadata input")
    }
    const metadata: CandidateMetadata = { candidate: await candidateManifest(request.candidateDirectory), candidate_artifact: request.candidateArtifact, context: request.trustedContext, producers: inputs.producers, relations: inputs.relations }
    await transaction.atomicFile(request.metadataPath, ".candidate-metadata-", `${canonicalizeJSON(metadata)}\n`)
  })
}

async function expectedControl(request: CandidateControlRequest): Promise<CandidateControl> {
  await validateReleaseCandidate(request.candidateDirectory)
  const metadata = validateMetadata(await readJSON(request.metadataPath))
  const inputs = await readCandidateInputs(request.producerDirectory, request.relationDirectory, request.schemaPath)
  return candidateControl(request, metadata, inputs, await candidateManifest(request.candidateDirectory))
}

export async function createCandidateControl(request: CandidateControlRequest): Promise<void> {
  await withReleaseTransaction(async (transaction) => {
    await transaction.atomicFile(request.controlPath, ".candidate-control-", `${canonicalizeJSON(await expectedControl(request))}\n`)
  })
}

export function readCandidateControl(value: unknown): CandidateControl {
  if (!isRecord(value)) {
    throw new Error("invalid candidate control")
  }
  exactKeys(value, ["candidate_artifact", "candidate_manifest", "commit", "producer_receipts", "relations", "repository", "run", "tag", "version", "workflow_blob"])
  if (!isRecord(value["candidate_artifact"]) || !isRecord(value["candidate_manifest"]) || !isRecord(value["run"])) {
    throw new Error("invalid candidate control")
  }
  const artifact = { digest: requireString(value["candidate_artifact"]["digest"], "control artifact digest"), id: requireString(value["candidate_artifact"]["id"], "control artifact ID") }
  const manifest = { manifest: requireString(value["candidate_manifest"]["manifest"], "control manifest"), manifest_sha256: requireString(value["candidate_manifest"]["manifest_sha256"], "control manifest sha256") }
  if (!isHash(artifact.digest) || !isPositiveID(artifact.id) || !isHash(manifest.manifest_sha256) || digest(manifest.manifest) !== manifest.manifest_sha256) {
    throw new Error("invalid candidate control identity")
  }
  const control = { candidate_artifact: artifact, candidate_manifest: manifest, commit: requireString(value["commit"], "control commit"), producer_receipts: receiptArray(value["producer_receipts"], "control producers").map(validateProducerReceipt), relations: receiptArray(value["relations"], "control relations").map(validateRelationReceipt), repository: requireString(value["repository"], "control repository"), run: { attempt: requireString(value["run"]["attempt"], "control run attempt"), id: requireString(value["run"]["id"], "control run ID"), workflow: requireString(value["run"]["workflow"], "control workflow") }, tag: requireString(value["tag"], "control tag"), version: requireString(value["version"], "control version"), workflow_blob: requireString(value["workflow_blob"], "control workflow blob") }
  assertTrustedContext({ commit: control.commit, repository: control.repository, runAttempt: control.run.attempt, runId: control.run.id, tag: control.tag, version: control.version, workflow: control.run.workflow, workflowBlob: control.workflow_blob })
  const producerNames = primaryNames(control.version)
  const relationNames = scannedNames(control.version).map((name) => `${name}.spdx.json`)
  if (control.producer_receipts.length !== producerNames.length || control.relations.length !== relationNames.length || control.producer_receipts.some((receipt, index) => receipt.name !== producerNames[index] || receipt.commit !== control.commit || receipt.version !== control.version) || control.relations.some((receipt, index) => receipt.predicate_name !== relationNames[index] || receipt.subject_name !== producerNames[index])) {
    throw new Error("incomplete candidate control")
  }
  return control
}

export async function validateCandidateControl(request: { readonly candidateDirectory: string; readonly controlPath: string; readonly expectedArtifact: CandidateArtifact }): Promise<void> {
  await validateReleaseCandidate(request.candidateDirectory)
  const control = readCandidateControl(await readJSON(request.controlPath))
  identical(control.candidate_artifact, request.expectedArtifact, "candidate artifact")
  identical(control.candidate_manifest, await candidateManifest(request.candidateDirectory), "candidate manifest")
}

export async function verifyCandidateControl(request: CandidateControlRequest): Promise<void> {
  identical(await readJSON(request.controlPath), await expectedControl(request), "candidate control")
}

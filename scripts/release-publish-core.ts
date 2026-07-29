import { createHash } from "node:crypto"
import { basename, join } from "node:path"
import { regularBytes } from "./release-candidate-data"
import type { CandidateControl } from "./release-candidate-control"
import { ReleasePublicationError } from "./release-publish-runtime"
import type { PublicationCandidate } from "./release-publish-runtime"

export type PublicationMode = "stage" | "finalize" | "recovery"
export type PublicationResult = { readonly kind: "published" | "recovered" | "staged"; readonly provenanceSubjects: readonly string[]; readonly sbomSubjects: readonly string[] }

export async function candidateFromControl(candidateDirectory: string, control: CandidateControl): Promise<PublicationCandidate> {
  const assets = control.producer_receipts.map((receipt) => ({ name: receipt.name, path: join(candidateDirectory, receipt.name), sha256: receipt.sha256 }))
  const sboms = await Promise.all(control.relations.map(async (relation) => {
    const path = join(candidateDirectory, relation.predicate_name)
    return { name: relation.predicate_name, path, sha256: createHash("sha256").update(await regularBytes(path)).digest("hex") }
  }))
  return { assets: [...assets, ...sboms, { name: "SHA256SUMS", path: join(candidateDirectory, "SHA256SUMS"), sha256: createHash("sha256").update(control.candidate_manifest.manifest).digest("hex") }], commit: control.commit, repository: control.repository, tag: control.tag, version: control.version, workflowBlob: control.workflow_blob }
}

export async function assertCandidate(candidate: PublicationCandidate): Promise<void> {
  if (candidate.tag !== `v${candidate.version}` || candidate.assets.length !== 12) {
    throw new ReleasePublicationError("invalid candidate publication identity")
  }
  await Promise.all(candidate.assets.map(async (asset) => {
    if (basename(asset.path) !== asset.name || createHash("sha256").update(await regularBytes(asset.path)).digest("hex") !== asset.sha256) {
      throw new ReleasePublicationError(`candidate asset mismatch: ${asset.name}`)
    }
  }))
}

export async function releasePublication(request: { readonly candidate: PublicationCandidate; readonly mode: PublicationMode }): Promise<PublicationResult> {
  await assertCandidate(request.candidate)
  const provenanceSubjects = request.candidate.assets.map((asset) => asset.name)
  const sbomSubjects = request.candidate.assets.filter((asset) => asset.name.endsWith(".spdx.json")).map((asset) => asset.name)
  if (request.mode === "finalize") return { kind: "published", provenanceSubjects, sbomSubjects }
  if (request.mode === "recovery") return { kind: "recovered", provenanceSubjects, sbomSubjects }
  return { kind: "staged", provenanceSubjects, sbomSubjects }
}

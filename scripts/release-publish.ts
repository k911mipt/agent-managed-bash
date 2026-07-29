import { appendFile, mkdtemp, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { parseStrictJSON, readJSON, regularBytes } from "./release-candidate-data"
import { readCandidateControl, validateCandidateControl } from "./release-candidate-control"
import { validateReleaseCandidate } from "./release-candidate-manifest"
import { assertSLSANpmProvenance, attestationState, provenancePredicateType, statementFromBundle } from "./release-publish-attestation"
import { candidateFromControl, releasePublication } from "./release-publish-core"
import type { JSONRecord, PublicationAsset, PublicationCandidate } from "./release-publish-runtime"
import { command, environment, expectedSRI, mutateThenRead, option, readCommand, record, ReleasePublicationError, string } from "./release-publish-runtime"
export { ReleasePublicationError } from "./release-publish-runtime"
export { releasePublication } from "./release-publish-core"

function npmProvenanceURL(dist: JSONRecord): string {
  const attestations = record(dist["attestations"], "npm attestations")
  const provenance = record(attestations["provenance"], "npm provenance")
  const direct = typeof attestations["url"] === "string" ? attestations["url"] : undefined
  const nested = typeof provenance["url"] === "string" ? provenance["url"] : undefined
  if (direct !== undefined && nested !== undefined && direct !== nested) throw new ReleasePublicationError("npm provenance URL mismatch")
  const url = nested ?? direct
  if (url === undefined || new URL(url).protocol !== "https:") throw new ReleasePublicationError("invalid npm provenance URL")
  return url
}

function sha512Digest(integrity: string): string {
  const encoded = integrity.match(/^sha512-([A-Za-z0-9+/]+={0,2})$/)?.[1]
  if (encoded === undefined) throw new ReleasePublicationError("invalid npm integrity")
  const digest = Buffer.from(encoded, "base64")
  if (digest.length !== 64 || digest.toString("base64") !== encoded) throw new ReleasePublicationError("invalid npm integrity")
  return digest.toString("hex")
}

async function auditNpm(npm: string, version: string): Promise<void> {
  const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-npm-audit-"))
  try {
    await writeFile(join(root, "package.json"), `${JSON.stringify({ dependencies: { "@k911mipt/opencode-agent-managed-bash": version }, name: "release-provenance-audit", private: true })}\n`)
    const install = await command(npm, ["--prefix", root, "install", "--ignore-scripts", "--no-audit", "--no-fund"])
    if (install.exitCode !== 0) throw new ReleasePublicationError("npm consumer installation failed")
    const audit = await command(npm, ["--prefix", root, "audit", "signatures", "--json", "--include-attestations"])
    if (audit.exitCode !== 0) throw new ReleasePublicationError("npm cryptographic provenance verification failed")
    const result = record(parseStrictJSON(audit.stdout), "npm audit signatures")
    if ("verified" in result || !Array.isArray(result["invalid"]) || !Array.isArray(result["missing"]) || result["invalid"].length !== 0 || result["missing"].length !== 0) throw new ReleasePublicationError("npm cryptographic provenance verification failed")
  } finally {
    await rm(root, { force: true, recursive: true })
  }
}

async function verifyNpmBundle(candidate: PublicationCandidate, dist: JSONRecord, expected: string): Promise<void> {
  const url = npmProvenanceURL(dist)
  const result = await command("curl", ["--fail", "--location", "--proto", "=https", "--proto-redir", "=https", "--silent", "--show-error", url])
  if (result.exitCode !== 0) throw new ReleasePublicationError("npm provenance bundle retrieval failed")
  const response = record(parseStrictJSON(result.stdout), "npm provenance bundle response")
  if (!Array.isArray(response["attestations"])) throw new ReleasePublicationError("invalid npm provenance bundle response")
  const matches = response["attestations"].map((value) => record(value, "npm provenance attestation")).filter((value) => string(value["predicateType"], "npm provenance predicate type") === provenancePredicateType)
  if (matches.length !== 1) throw new ReleasePublicationError("invalid npm provenance bundle")
  const statement = statementFromBundle(record(matches[0], "npm provenance attestation")["bundle"])
  const subject = statement.subjects[0]
  if (subject === undefined || statement.subjects.length !== 1 || subject.name !== `pkg:npm/%40k911mipt/opencode-agent-managed-bash@${candidate.version}` || subject.digest["sha512"] !== sha512Digest(expected) || Object.keys(subject.digest).length !== 1) throw new ReleasePublicationError("npm provenance subject mismatch")
  assertSLSANpmProvenance(statement, candidate)
}

async function readNpm(candidate: PublicationCandidate, npm: string, tarball: PublicationAsset): Promise<void> {
  const value = await readCommand(npm, ["view", `@k911mipt/opencode-agent-managed-bash@${candidate.version}`, "--json"])
  if (value === undefined) {
    throw new ReleasePublicationError("npm version is absent")
  }
  const packageValue = record(value, "npm envelope")
  const dist = record(packageValue["dist"], "npm dist")
  const expected = expectedSRI(await regularBytes(tarball.path))
  if (string(packageValue["name"], "npm name") !== "@k911mipt/opencode-agent-managed-bash" || string(packageValue["version"], "npm version") !== candidate.version || string(dist["integrity"], "npm integrity") !== expected) throw new ReleasePublicationError("npm SRI mismatch")
  await auditNpm(npm, candidate.version)
  await verifyNpmBundle(candidate, dist, expected)
}

async function reconcileNpm(candidate: PublicationCandidate, npm: string, bootstrapRequested: boolean, bootstrapAllowed: boolean): Promise<void> {
  const tarball = candidate.assets.find((asset) => asset.name.endsWith(".tgz"))
  if (tarball === undefined) {
    throw new ReleasePublicationError("missing npm candidate tarball")
  }
  const current = await readCommand(npm, ["view", `@k911mipt/opencode-agent-managed-bash@${candidate.version}`, "--json"])
  if (current === undefined) {
    if (bootstrapRequested && !bootstrapAllowed) throw new ReleasePublicationError("invalid npm bootstrap credentials")
    const token = bootstrapAllowed ? environment("NPM_TOKEN") : undefined
    await mutateThenRead(npm, ["publish", tarball.path, "--access", "public", "--provenance"], async () => readNpm(candidate, npm, tarball), token === undefined ? {} : { NPM_TOKEN: token })
    return
  }
  if (bootstrapRequested || process.env["NPM_TOKEN"] !== undefined || process.env["NPM_BOOTSTRAP_VERSION"] !== undefined) throw new ReleasePublicationError("stale npm bootstrap credentials")
  await readNpm(candidate, npm, tarball)
}

async function readRelease(candidate: PublicationCandidate, gh: string, allowDraft: boolean): Promise<void> {
  const value = await readCommand(gh, ["release", "view", candidate.tag, "--repo", candidate.repository, "--json", "tagName,isDraft,isImmutable,targetCommitish,assets"])
  if (value === undefined) {
    throw new ReleasePublicationError("GitHub release is absent")
  }
  const release = record(value, "GitHub release")
  const assets = Array.isArray(release["assets"]) ? release["assets"].map((asset) => record(asset, "GitHub release asset")) : []
  const expected = new Map(candidate.assets.map((asset) => [asset.name, asset.sha256]))
  if (string(release["tagName"], "GitHub release tag") !== candidate.tag || string(release["targetCommitish"], "GitHub release commit") !== candidate.commit || typeof release["isDraft"] !== "boolean" || release["isDraft"] !== allowDraft || release["isImmutable"] !== !allowDraft || assets.length !== expected.size || assets.some((asset) => expected.get(string(asset["name"], "GitHub asset name")) !== githubDigest(string(asset["digest"], "GitHub asset digest")))) {
    throw new ReleasePublicationError("GitHub draft or release mismatch")
  }
}

async function reconcileDraft(candidate: PublicationCandidate, gh: string): Promise<boolean> {
  const current = await readCommand(gh, ["release", "view", candidate.tag, "--repo", candidate.repository, "--json", "tagName,isDraft,isImmutable,targetCommitish,assets"])
  if (current === undefined) {
    await mutateThenRead(gh, ["release", "create", candidate.tag, "--repo", candidate.repository, "--draft", "--target", candidate.commit, ...candidate.assets.map((asset) => asset.path)], async () => readRelease(candidate, gh, true))
    return false
  }
  const release = record(current, "GitHub release")
  if (release["isDraft"] === false) {
    await readRelease(candidate, gh, false)
    return true
  }
  if (string(release["tagName"], "GitHub release tag") !== candidate.tag || string(release["targetCommitish"], "GitHub release commit") !== candidate.commit || release["isImmutable"] !== false) {
    throw new ReleasePublicationError("GitHub draft identity mismatch")
  }
  const expected = new Map(candidate.assets.map((asset) => [asset.name, asset.sha256]))
  const actual = Array.isArray(release["assets"]) ? release["assets"].map((asset) => record(asset, "GitHub release asset")) : []
  const actualNames = new Set<string>()
  for (const asset of actual) {
    const name = string(asset["name"], "GitHub asset name")
    if (actualNames.has(name) || expected.get(name) !== githubDigest(string(asset["digest"], "GitHub asset digest"))) {
      throw new ReleasePublicationError("GitHub draft asset mismatch")
    }
    actualNames.add(name)
  }
  for (const asset of candidate.assets.filter((candidateAsset) => !actualNames.has(candidateAsset.name))) {
    await mutateThenRead(gh, ["release", "upload", candidate.tag, "--repo", candidate.repository, asset.path], async () => {
      const after = await readCommand(gh, ["release", "view", candidate.tag, "--repo", candidate.repository, "--json", "tagName,isDraft,isImmutable,targetCommitish,assets"])
      if (after === undefined) throw new ReleasePublicationError("GitHub draft vanished")
      const afterRelease = record(after, "GitHub release")
      if (afterRelease["isDraft"] !== true || afterRelease["isImmutable"] !== false) throw new ReleasePublicationError("GitHub draft state changed")
    })
  }
  await readRelease(candidate, gh, true)
  return false
}

function githubDigest(value: string): string {
  const match = /^sha256:([a-f0-9]{64})$/.exec(value)
  if (match?.[1] === undefined) throw new ReleasePublicationError("invalid GitHub asset digest")
  return match[1]
}

export async function main(arguments_: readonly string[]): Promise<void> {
  const mode = arguments_[0]
  if (mode === "guard") {
    const npm = process.env["RELEASE_NPM_BIN"] ?? "npm"
    const gh = process.env["RELEASE_GH_BIN"] ?? "gh"
    const repository = environment("RELEASE_REPOSITORY")
    const tag = environment("RELEASE_TAG")
    const version = environment("RELEASE_VERSION")
    if (await readCommand(npm, ["view", `@k911mipt/opencode-agent-managed-bash@${version}`, "--json"]) !== undefined || await readCommand(gh, ["release", "view", tag, "--repo", repository, "--json", "tagName"]) !== undefined) throw new ReleasePublicationError("public release state already exists")
    return
  }
  if (mode !== "stage" && mode !== "finalize" && mode !== "recovery") {
    throw new ReleasePublicationError("expected guard, stage, finalize, or recovery command")
  }
  const bootstrapMarker = process.env["RELEASE_FIRST_PUBLISH_BOOTSTRAP"]
  const bootstrapRequested = bootstrapMarker !== undefined && bootstrapMarker.length !== 0
  const bootstrapAllowed = mode === "stage" && process.env["RELEASE_FIRST_PUBLISH_BOOTSTRAP"] === "true" && process.env["NPM_TOKEN"] !== undefined && process.env["NPM_BOOTSTRAP_VERSION"] === process.env["RELEASE_VERSION"]
  if ((process.env["NPM_TOKEN"] !== undefined || process.env["NPM_BOOTSTRAP_VERSION"] !== undefined || bootstrapRequested) && !bootstrapAllowed) {
    throw new ReleasePublicationError("stale npm bootstrap credentials")
  }
  const candidateDirectory = option(arguments_, "--candidate")
  const controlPath = option(arguments_, "--control")
  const control = readCandidateControl(await readJSON(controlPath))
  await validateReleaseCandidate(candidateDirectory)
  await validateCandidateControl({ candidateDirectory, controlPath, expectedArtifact: control.candidate_artifact })
  const candidate = await candidateFromControl(candidateDirectory, control)
  if (environment("RELEASE_REPOSITORY") !== candidate.repository || environment("RELEASE_TAG") !== candidate.tag || environment("RELEASE_COMMIT") !== candidate.commit || environment("RELEASE_VERSION") !== candidate.version) {
    throw new ReleasePublicationError("trusted release context mismatch")
  }
  if (mode === "recovery" && (environment("RELEASE_EVENT_NAME") !== "workflow_dispatch" || environment("RELEASE_RECOVERY_RUN_ID") !== control.run.id || environment("RELEASE_REF") !== `refs/tags/${candidate.tag}` || environment("RELEASE_SHA") !== candidate.commit || environment("RELEASE_WORKFLOW") !== control.run.workflow || environment("RELEASE_WORKFLOW_BLOB") !== candidate.workflowBlob)) {
    throw new ReleasePublicationError("invalid recovery context")
  }
  await releasePublication({ candidate, mode })
  if (mode === "recovery") return
  const gh = process.env["RELEASE_GH_BIN"] ?? "gh"
  const npm = process.env["RELEASE_NPM_BIN"] ?? "npm"
  if (mode === "stage") {
    await reconcileNpm(candidate, npm, bootstrapRequested, bootstrapAllowed)
    const terminal = await reconcileDraft(candidate, gh)
    const attestations = await attestationState(candidate, control, command)
    if (terminal && !attestations.complete) throw new ReleasePublicationError("immutable release has incomplete attestations")
    const output = process.env["GITHUB_OUTPUT"]
    if (output !== undefined) await appendFile(output, `terminal=${terminal}\nprovenance<<EOF\n${attestations.missingProvenance.map((name) => `candidate/${name}`).join("\n")}\nEOF\nsbom=${attestations.missingSBOM.join(",")}\n`)
    return
  }
  await readNpm(candidate, npm, candidate.assets.find((asset) => asset.name.endsWith(".tgz")) ?? (() => { throw new ReleasePublicationError("missing npm candidate tarball") })())
  await readRelease(candidate, gh, true)
  const attestations = await attestationState(candidate, control, command)
  if (!attestations.complete) throw new ReleasePublicationError("release attestations are incomplete")
  await mutateThenRead(gh, ["release", "edit", candidate.tag, "--repo", candidate.repository, "--draft=false"], async () => readRelease(candidate, gh, false))
}

if (import.meta.main) {
  await main(Bun.argv.slice(2))
}

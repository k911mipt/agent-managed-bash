import { createHash } from "node:crypto"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { canonicalizeJSON, isRecord, parseStrictJSON, readJSON, regularBytes } from "./release-candidate-data"
import type { CandidateControl } from "./release-candidate-control"

export const provenancePredicateType = "https://slsa.dev/provenance/v1"
export const sbomPredicateType = "https://spdx.dev/Document/v2.3"

type Asset = { readonly name: string; readonly path: string; readonly sha256: string }
type Candidate = { readonly assets: readonly Asset[]; readonly commit: string; readonly repository: string; readonly tag: string; readonly version: string }
type Subject = { readonly digest: Readonly<Record<string, string>>; readonly name: string }
export type Statement = { readonly predicate: unknown; readonly predicateType: string; readonly subjects: readonly Subject[] }
type VerifiedStatement = Statement & { readonly bundleDigest: string }
type Command = (executable: string, arguments_: readonly string[]) => Promise<{ readonly exitCode: number; readonly stderr: string; readonly stdout: string }>

function requiredString(value: unknown, field: string): string {
  if (typeof value !== "string" || value.length === 0) throw new Error(`invalid ${field}`)
  return value
}

function exactDigest(value: unknown, field: string): Readonly<Record<string, string>> {
  const digest = record(value, field)
  const entries = Object.entries(digest)
  if (entries.length !== 1) throw new Error(`invalid ${field}`)
  const entry = entries[0]
  if (entry === undefined) throw new Error(`invalid ${field}`)
  const [algorithm, rawValue] = entry
  const value_ = requiredString(rawValue, field)
  if ((algorithm !== "sha256" || !/^[a-f0-9]{64}$/.test(value_)) && (algorithm !== "sha512" || !/^[a-f0-9]{128}$/.test(value_))) throw new Error(`invalid ${field}`)
  return { [algorithm]: value_ }
}

function record(value: unknown, field: string): Record<string, unknown> {
  if (!isRecord(value)) throw new Error(`invalid ${field}`)
  return value
}

export function statementFromBundle(bundle: unknown): Statement {
  const envelope = record(record(bundle, "verified DSSE bundle")["dsseEnvelope"], "DSSE envelope")
  if (requiredString(envelope["payloadType"], "DSSE payload type") !== "application/vnd.in-toto+json" || !Array.isArray(envelope["signatures"]) || envelope["signatures"].length === 0) throw new Error("invalid DSSE envelope")
  const encoded = requiredString(envelope["payload"], "DSSE payload")
  if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)) throw new Error("invalid DSSE payload encoding")
  const payload = Buffer.from(encoded, "base64")
  if (payload.toString("base64") !== encoded) throw new Error("non-canonical DSSE payload encoding")
  const statement = record(parseStrictJSON(new TextDecoder("utf-8", { fatal: true }).decode(payload)), "in-toto statement")
  if (Object.keys(statement).length !== 4 || requiredString(statement["_type"], "statement type") !== "https://in-toto.io/Statement/v1" || !Array.isArray(statement["subject"])) throw new Error("invalid in-toto statement")
  const subjects = statement["subject"].map((value) => {
    const subject = record(value, "in-toto subject")
    if (Object.keys(subject).length !== 2) throw new Error("invalid in-toto subject")
    return { digest: exactDigest(subject["digest"], "statement subject digest"), name: requiredString(subject["name"], "statement subject name") }
  })
  if (subjects.length === 0) throw new Error("missing in-toto subjects")
  return { predicate: statement["predicate"], predicateType: requiredString(statement["predicateType"], "statement predicate type"), subjects }
}

function exactSubject(subject: Subject, asset: Asset): boolean {
  return subject.name === asset.name && subject.digest["sha256"] === asset.sha256 && Object.keys(subject.digest).length === 1
}

function assertSLSAProvenance(statement: Statement, candidate: Candidate, builder: string): string {
  if (statement.predicateType !== provenancePredicateType) throw new Error("invalid provenance predicate")
  const predicate = record(statement.predicate, "provenance predicate")
  const build = record(predicate["buildDefinition"], "provenance build definition")
  const workflow = record(record(build["externalParameters"], "provenance external parameters")["workflow"], "provenance workflow")
  const ref = `refs/tags/${candidate.tag}`
  if (requiredString(workflow["repository"], "provenance repository") !== `https://github.com/${candidate.repository}` || requiredString(workflow["path"], "provenance workflow path") !== ".github/workflows/release.yml" || requiredString(workflow["ref"], "provenance ref") !== ref) throw new Error("provenance workflow claims mismatch")
  const dependencies = build["resolvedDependencies"]
  if (!Array.isArray(dependencies) || dependencies.length !== 1) throw new Error("invalid provenance dependencies")
  const dependency = record(dependencies[0], "provenance dependency")
  if (requiredString(dependency["uri"], "provenance dependency URI") !== `git+https://github.com/${candidate.repository}@${ref}` || requiredString(record(dependency["digest"], "provenance dependency digest")["gitCommit"], "provenance SHA") !== candidate.commit) throw new Error("provenance dependency claims mismatch")
  const run = record(predicate["runDetails"], "provenance run details")
  const invocation = requiredString(record(run["metadata"], "provenance metadata")["invocationId"], "provenance invocation ID")
  if (requiredString(record(run["builder"], "provenance builder")["id"], "provenance builder ID") !== builder || !invocation.startsWith(`https://github.com/${candidate.repository}/actions/runs/`)) throw new Error("provenance run claims mismatch")
  return invocation
}

export function assertSLSAWorkflowProvenance(statement: Statement, candidate: Candidate): void {
  assertSLSAProvenance(statement, candidate, `https://github.com/${candidate.repository}/.github/workflows/release.yml@refs/tags/${candidate.tag}`)
  const build = record(record(statement.predicate, "provenance predicate")["buildDefinition"], "provenance build definition")
  const github = record(record(build["internalParameters"], "provenance internal parameters")["github"], "provenance GitHub parameters")
  if (requiredString(github["runner_environment"], "provenance runner environment") !== "github-hosted") throw new Error("provenance environment mismatch")
}

export function assertSLSANpmProvenance(statement: Statement, candidate: Candidate): string {
  return assertSLSAProvenance(statement, candidate, "https://github.com/actions/runner/github-hosted")
}

export function assertNpmCertificateVerification(value: unknown, candidate: Candidate, invocation: string): void {
  if (!Array.isArray(value) || value.length !== 1) throw new Error("invalid npm certificate verification")
  const verification = record(record(value[0], "npm certificate verification")["verificationResult"], "npm verification result")
  const certificate = record(record(verification["signature"], "npm verification signature")["certificate"], "npm verification certificate")
  const ref = `refs/tags/${candidate.tag}`
  const signer = `https://github.com/${candidate.repository}/.github/workflows/release.yml@${ref}`
  const expected = {
    buildConfigDigest: candidate.commit, buildConfigURI: signer, buildSignerDigest: candidate.commit, buildSignerURI: signer,
    githubWorkflowRef: ref, githubWorkflowRepository: candidate.repository, githubWorkflowSHA: candidate.commit,
    issuer: "https://token.actions.githubusercontent.com", runInvocationURI: invocation, runnerEnvironment: "github-hosted",
    sourceRepositoryDigest: candidate.commit, sourceRepositoryRef: ref, sourceRepositoryURI: `https://github.com/${candidate.repository}`,
    subjectAlternativeName: signer,
  }
  if (Object.entries(expected).some(([field, expectedValue]) => requiredString(certificate[field], `npm certificate ${field}`) !== expectedValue)) throw new Error("npm certificate claims mismatch")
}

async function verifiedStatements(asset: Asset, candidate: Candidate, command: Command): Promise<readonly VerifiedStatement[]> {
  const result = await command("gh", ["api", `repos/${candidate.repository}/attestations/sha256:${asset.sha256}`])
  if (result.exitCode === 1 && result.stderr.includes("not found")) return []
  if (result.exitCode !== 0) throw new Error("attestation enumeration failed")
  const response = record(parseStrictJSON(result.stdout), "attestation response")
  if (!Array.isArray(response["attestations"])) throw new Error("invalid attestation response")
  const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-attestation-"))
  try {
    const seen = new Set<string>()
    const statements: VerifiedStatement[] = []
    for (const [index, value] of response["attestations"].entries()) {
      const bundle = record(record(value, "attestation")["bundle"], "attestation bundle")
      const bundleDigest = createHash("sha256").update(canonicalizeJSON(bundle)).digest("hex")
      if (seen.has(bundleDigest)) throw new Error("duplicate attestation bundle")
      seen.add(bundleDigest)
      const path = join(root, `${index}.json`)
      await writeFile(path, `${canonicalizeJSON(bundle)}\n`)
      const statement = statementFromBundle(bundle)
      if (statement.predicateType !== provenancePredicateType && statement.predicateType !== sbomPredicateType) throw new Error("unexpected attestation predicate")
      const verified = await command("gh", ["attestation", "verify", asset.path, "--repo", candidate.repository, "--bundle", path, "--predicate-type", statement.predicateType])
      if (verified.exitCode !== 0) throw new Error("cryptographic attestation verification failed")
      if (!statement.subjects.some((subject) => exactSubject(subject, asset))) throw new Error("attestation subject mismatch")
      statements.push({ ...statement, bundleDigest })
    }
    return statements
  } finally {
    await rm(root, { force: true, recursive: true })
  }
}

function uniqueStatements(statements: readonly VerifiedStatement[]): readonly VerifiedStatement[] {
  const unique = new Map<string, VerifiedStatement>()
  for (const statement of statements) unique.set(statement.bundleDigest, statement)
  return [...unique.values()]
}

function coveredProvenanceSubjects(statements: readonly VerifiedStatement[], candidate: Candidate): ReadonlySet<string> {
  const expected = new Map(candidate.assets.map((asset) => [asset.name, asset.sha256]))
  const covered = new Set<string>()
  for (const statement of statements) {
    assertSLSAWorkflowProvenance(statement, candidate)
    const local = new Set<string>()
    for (const subject of statement.subjects) {
      const digest = subject.digest["sha256"]
      if (digest === undefined || Object.keys(subject.digest).length !== 1 || expected.get(subject.name) !== digest || local.has(subject.name) || covered.has(subject.name)) throw new Error("provenance subject set mismatch")
      local.add(subject.name)
      covered.add(subject.name)
    }
  }
  return covered
}

export async function attestationState(candidate: Candidate, control: CandidateControl, command: Command): Promise<{ readonly complete: boolean; readonly missingProvenance: readonly string[]; readonly missingSBOM: readonly string[] }> {
  const outcomes = await Promise.allSettled(candidate.assets.map((asset) => verifiedStatements(asset, candidate, command)))
  const failure = outcomes.find((outcome) => outcome.status === "rejected")
  if (failure?.status === "rejected") throw failure.reason
  const unique = uniqueStatements(outcomes.flatMap((outcome) => outcome.status === "fulfilled" ? outcome.value : []))
  if (unique.some((statement) => statement.predicateType !== provenancePredicateType && statement.predicateType !== sbomPredicateType)) throw new Error("unexpected attestation predicate")
  const provenance = unique.filter((statement) => statement.predicateType === provenancePredicateType)
  const covered = coveredProvenanceSubjects(provenance, candidate)
  const missingProvenance = candidate.assets.filter((asset) => !covered.has(asset.name)).map((asset) => asset.name)
  const sboms = unique.filter((statement) => statement.predicateType === sbomPredicateType)
  const sbomBySubject = new Map<string, VerifiedStatement>()
  for (const statement of sboms) {
    if (statement.subjects.length !== 1) throw new Error("invalid SBOM subject set")
    const subject = statement.subjects[0]
    if (subject === undefined) throw new Error("invalid SBOM subject set")
    const asset = candidate.assets.find((value) => exactSubject(subject, value))
    const relation = asset === undefined ? undefined : control.relations.find((value) => value.subject_name === asset.name)
    if (asset === undefined || relation === undefined || sbomBySubject.has(asset.name)) throw new Error("unexpected or duplicate SBOM attestation")
    if (createHash("sha256").update(canonicalizeJSON(statement.predicate)).digest("hex") !== relation.predicate_sha256 || canonicalizeJSON(statement.predicate) !== canonicalizeJSON(await readJSON(asset.path.replace(/\.tar\.gz$|\.tgz$/, "$&.spdx.json")))) throw new Error("SBOM attestation predicate mismatch")
    sbomBySubject.set(asset.name, statement)
  }
  const missingSBOM = control.relations.filter((relation) => !sbomBySubject.has(relation.subject_name)).map((relation) => relation.subject_name)
  return { complete: missingProvenance.length === 0 && missingSBOM.length === 0, missingProvenance, missingSBOM }
}

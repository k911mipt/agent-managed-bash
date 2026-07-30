import { mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { tmpdir } from "node:os"
import { assembleReleaseCandidate, createCandidateControl, createCandidateMetadata } from "./release-candidate"
import { fixtureCommit, fixtureVersion, writeCandidateFixture } from "./release-candidate-fixtures.test"

const schemaPath = resolve(import.meta.dir, "../schemas/spdx-schema.json")
const publishPath = resolve(import.meta.dir, "release-publish.ts")
const artifact = { digest: "a".repeat(64), id: "123" }

export type ProcessResult = { readonly exitCode: number; readonly stderr: string; readonly stdout: string }

export function fakeStatePath(environment: Readonly<Record<string, string>>): string {
  const path = environment["FAKE_RELEASE_STATE"]
  if (path === undefined) throw new Error("fake release state is missing")
  return path
}

export async function runPublication(arguments_: readonly string[], environment: Readonly<Record<string, string>>): Promise<ProcessResult> {
  const child = Bun.spawn({ cmd: [process.execPath, publishPath, ...arguments_], env: { ...process.env, ...environment }, stderr: "pipe", stdout: "pipe" })
  if (typeof child.stderr === "number" || typeof child.stdout === "number" || child.stderr === undefined || child.stdout === undefined) throw new Error("publication test child output is not piped")
  const [exitCode, stderr, stdout] = await Promise.all([child.exited, new Response(child.stderr).text(), new Response(child.stdout).text()])
  return { exitCode, stderr, stdout }
}

export async function setupPublication(root: string): Promise<Readonly<Record<string, string>>> {
  await writeCandidateFixture(root)
  await assembleReleaseCandidate({ outputDirectory: join(root, "candidate"), producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), schemaPath })
  await createCandidateMetadata({ candidateArtifact: artifact, candidateDirectory: join(root, "candidate"), metadataPath: join(root, "metadata.json"), producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), schemaPath, trustedContext: { commit: fixtureCommit, repository: "k911mipt/agent-managed-bash", runAttempt: "1", runId: "456", tag: `v${fixtureVersion}`, version: fixtureVersion, workflow: "release.yml", workflowBlob: fixtureCommit } })
  await createCandidateControl({ candidateArtifact: artifact, candidateDirectory: join(root, "candidate"), commit: fixtureCommit, controlPath: join(root, "control", "CANDIDATE-RECEIPT.json"), metadataPath: join(root, "metadata.json"), producerDirectory: join(root, "producers"), relationDirectory: join(root, "relations"), repository: "k911mipt/agent-managed-bash", run: { attempt: "1", id: "456", workflow: "release.yml" }, schemaPath, tag: `v${fixtureVersion}`, trustedContext: { commit: fixtureCommit, repository: "k911mipt/agent-managed-bash", runAttempt: "1", runId: "456", tag: `v${fixtureVersion}`, version: fixtureVersion, workflow: "release.yml", workflowBlob: fixtureCommit }, version: fixtureVersion })
  const bin = join(root, "bin")
  const state = join(root, "state.json")
  await mkdir(bin)
  await writeFile(state, "{}")
  const fake = `#!/usr/bin/env bun
const [kind, ...arguments_] = Bun.argv.slice(2)
const statePath = process.env.FAKE_RELEASE_STATE
const state = JSON.parse(await Bun.file(statePath).text())
const save = async () => Bun.write(statePath, JSON.stringify(state))
await Bun.write(statePath + ".child-" + process.pid, JSON.stringify({ arguments_, hasNodeAuthToken: process.env.NODE_AUTH_TOKEN !== undefined, kind }))
const packageName = "@k911mipt/opencode-agent-managed-bash"
const source = state.githubClaims ?? {}
const predicate = () => ({ buildDefinition: { buildType: "https://actions.github.io/buildtypes/workflow/v1", externalParameters: { workflow: { path: source.path ?? ".github/workflows/release.yml", ref: source.ref ?? \`refs/tags/\${process.env.RELEASE_TAG}\`, repository: source.repository ?? \`https://github.com/\${process.env.RELEASE_REPOSITORY}\` } }, internalParameters: { github: { runner_environment: source.environment ?? "github-hosted" } }, resolvedDependencies: [{ digest: { gitCommit: source.sha ?? process.env.RELEASE_COMMIT }, uri: source.uri ?? \`git+https://github.com/\${process.env.RELEASE_REPOSITORY}@refs/tags/\${process.env.RELEASE_TAG}\` }] }, runDetails: { builder: { id: source.builder ?? \`https://github.com/\${process.env.RELEASE_REPOSITORY}/.github/workflows/release.yml@refs/tags/\${process.env.RELEASE_TAG}\` }, metadata: { invocationId: \`https://github.com/\${process.env.RELEASE_REPOSITORY}/actions/runs/789/attempts/1\` } } })
const statement = (predicateType, value, subjects) => ({ dsseEnvelope: { payload: Buffer.from(JSON.stringify({ _type: "https://in-toto.io/Statement/v1", predicate: value, predicateType, subject: subjects })).toString("base64"), payloadType: "application/vnd.in-toto+json", signatures: [{ keyid: "test", sig: "verified" }] } })
const npmBundle = () => {
  const integrity = state.npm.dist.integrity
  const sha512 = Buffer.from(integrity.slice("sha512-".length), "base64").toString("hex")
  const npmPredicate = predicate()
  npmPredicate.runDetails.builder.id = state.npmBuilder ?? "https://github.com/actions/runner/github-hosted"
  return { attestations: [{ bundle: statement("https://slsa.dev/provenance/v1", npmPredicate, [{ digest: { sha512 }, name: state.npmPurl ?? \`pkg:npm/%40k911mipt/opencode-agent-managed-bash@\${process.env.RELEASE_VERSION}\` }]), predicateType: "https://slsa.dev/provenance/v1" }] }
}
if (kind === "npm") {
  const args = arguments_[0] === "--prefix" ? arguments_.slice(2) : arguments_
  if (args[0] === "view") { if (state.npm === undefined || state.npmDelay > 0) { if (state.npmDelay > 0) { state.npmDelay -= 1; await save() }; console.error("E404"); process.exit(1) }; console.log(JSON.stringify(state.npm)); process.exit(0) }
  if (args[0] === "install") { state.npmInstallCount = (state.npmInstallCount ?? 0) + 1; await save(); process.exit(0) }
  if (args[0] === "publish") { const bytes = await Bun.file(args[1]).arrayBuffer(); const integrity = \`sha512-\${new Bun.CryptoHasher("sha512").update(bytes).digest("base64")}\`; state.npm = { name: packageName, version: process.env.RELEASE_VERSION, dist: { attestations: { provenance: { url: state.npmBundleURL ?? "https://registry.test/npm-provenance" } }, integrity } }; state.npmBundle = npmBundle(); state.npmDelay = state.npmDelayOnPublish ?? 0; await save(); if (state.ambiguous === "npm-publish") process.exit(1); process.exit(0) }
  if (args[0] === "audit") { if (state.npm === undefined) process.exit(1); console.log(JSON.stringify({ invalid: state.npmInvalid ?? [], missing: state.npmMissing ?? [] })); process.exit(state.npmAuditExitCode ?? 0) }
}
if (kind === "curl") { const url = arguments_.at(-1); if (url !== state.npm?.dist?.attestations?.provenance?.url) process.exit(2); console.log(JSON.stringify(state.npmBundle)); process.exit(0) }
if (kind === "gh") {
  if (arguments_[0] === "release" && arguments_[1] === "view") { if (state.release === undefined || state.releaseDelay > 0) { if (state.releaseDelay > 0) { state.releaseDelay -= 1; await save() }; console.error("not found"); process.exit(1) }; console.log(JSON.stringify(state.release)); process.exit(0) }
  if (arguments_[0] === "release" && arguments_[1] === "create") { const target = arguments_[arguments_.indexOf("--target") + 1]; const paths = arguments_.slice(arguments_.indexOf("--target") + 2).filter((value) => !value.startsWith("--")); state.release = { tagName: arguments_[2], isDraft: true, isImmutable: false, targetCommitish: target, assets: await Promise.all(paths.map(async (path) => ({ name: path.split("/").at(-1), digest: \`sha256:\${new Bun.CryptoHasher("sha256").update(await Bun.file(path).arrayBuffer()).digest("hex")}\` }))) }; state.releaseDelay = state.releaseDelayOnCreate ?? 0; await save(); if (state.ambiguous === "release-create") process.exit(1); process.exit(0) }
  if (arguments_[0] === "release" && arguments_[1] === "upload") { const path = arguments_.at(-1); state.release.assets.push({ name: path.split("/").at(-1), digest: \`sha256:\${new Bun.CryptoHasher("sha256").update(await Bun.file(path).arrayBuffer()).digest("hex")}\` }); await save(); if (state.ambiguous === "release-upload") process.exit(1); process.exit(0) }
  if (arguments_[0] === "release" && arguments_[1] === "edit") { state.release.isDraft = false; state.release.isImmutable = true; await save(); if (state.ambiguous === "release-edit") process.exit(1); process.exit(0) }
  if (arguments_[0] === "api") { const digest = arguments_[1].split("sha256:").at(-1); console.log(JSON.stringify({ attestations: state.attestations?.[digest] ?? [] })); process.exit(0) }
    if (arguments_[0] === "attestation" && arguments_[1] === "verify") { if (state.requiredPredicateType !== undefined && arguments_[arguments_.indexOf("--predicate-type") + 1] !== state.requiredPredicateType) process.exit(1); process.exit(state.attestationVerifyExitCode ?? 0) }
}
process.exit(2)
`
  await writeFile(join(bin, "npm"), fake.replace('const [kind, ...arguments_] = Bun.argv.slice(2)', 'const [kind, ...arguments_] = ["npm", ...Bun.argv.slice(2)]'))
  await writeFile(join(bin, "curl"), fake.replace('const [kind, ...arguments_] = Bun.argv.slice(2)', 'const [kind, ...arguments_] = ["curl", ...Bun.argv.slice(2)]'))
  await writeFile(join(bin, "gh"), fake.replace('const [kind, ...arguments_] = Bun.argv.slice(2)', 'const [kind, ...arguments_] = ["gh", ...Bun.argv.slice(2)]'))
  await Promise.all([Bun.$`chmod +x ${join(bin, "npm")}`.quiet(), Bun.$`chmod +x ${join(bin, "curl")}`.quiet(), Bun.$`chmod +x ${join(bin, "gh")}`.quiet()])
  return { FAKE_RELEASE_STATE: state, RELEASE_COMMIT: fixtureCommit, RELEASE_CURL_BIN: join(bin, "curl"), RELEASE_EVENT_NAME: "workflow_dispatch", RELEASE_GH_BIN: join(bin, "gh"), RELEASE_NPM_BIN: join(bin, "npm"), RELEASE_RECOVERY_RUN_ID: "456", RELEASE_REF: `refs/tags/v${fixtureVersion}`, RELEASE_REPOSITORY: "k911mipt/agent-managed-bash", RELEASE_SHA: fixtureCommit, RELEASE_TAG: `v${fixtureVersion}`, RELEASE_VERSION: fixtureVersion, RELEASE_WORKFLOW: "release.yml", RELEASE_WORKFLOW_BLOB: fixtureCommit }
}

export async function addExactAttestations(root: string, environment: Readonly<Record<string, string>>): Promise<void> {
  const state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
  const control = JSON.parse(await readFile(join(root, "control", "CANDIDATE-RECEIPT.json"), "utf8"))
  const source = state.githubClaims ?? {}
  const predicate = { buildDefinition: { buildType: "https://actions.github.io/buildtypes/workflow/v1", externalParameters: { workflow: { path: source.path ?? ".github/workflows/release.yml", ref: source.ref ?? `refs/tags/v${fixtureVersion}`, repository: source.repository ?? "https://github.com/k911mipt/agent-managed-bash" } }, internalParameters: { github: { runner_environment: source.environment ?? "github-hosted" } }, resolvedDependencies: [{ digest: { gitCommit: source.sha ?? fixtureCommit }, uri: source.uri ?? `git+https://github.com/k911mipt/agent-managed-bash@refs/tags/v${fixtureVersion}` }] }, runDetails: { builder: { id: source.builder ?? `https://github.com/k911mipt/agent-managed-bash/.github/workflows/release.yml@refs/tags/v${fixtureVersion}` }, metadata: { invocationId: "https://github.com/k911mipt/agent-managed-bash/actions/runs/789/attempts/1" } } }
  const statement = (predicateType: string, value: unknown, subjects: readonly unknown[]) => ({ dsseEnvelope: { payload: Buffer.from(JSON.stringify({ _type: "https://in-toto.io/Statement/v1", predicate: value, predicateType, subject: subjects })).toString("base64"), payloadType: "application/vnd.in-toto+json", signatures: [{ keyid: "test", sig: "verified" }] } })
  const subjects = await Promise.all(state.release.assets.map(async (asset: { readonly digest: string; readonly name: string }) => ({ digest: { sha256: asset.digest.replace("sha256:", "") }, name: asset.name })))
  if (state.githubSubjectShape === "missing") subjects.pop()
  if (state.githubSubjectShape === "extra") subjects.push({ digest: { sha256: "f".repeat(64) }, name: "unexpected" })
  if (state.githubSubjectShape === "duplicate") subjects.push(subjects[0])
  const provenance = statement("https://slsa.dev/provenance/v1", predicate, subjects)
  const bundles: Record<string, unknown[]> = {}
  for (const asset of state.release.assets) {
    const name = asset.name as string
    const digest = (asset.digest as string).replace("sha256:", "")
    bundles[digest] = [{ bundle: provenance }]
    const relation = control.relations.find((value: { subject_name: string }) => value.subject_name === name)
    if (relation !== undefined) bundles[digest]!.push({ bundle: statement(state.sbomPredicateType ?? "https://spdx.dev/Document/v2.3", JSON.parse(await readFile(join(root, "candidate", `${name}.spdx.json`), "utf8")), [{ digest: { sha256: digest }, name }]) })
  }
  state.attestations = bundles
  await writeFile(fakeStatePath(environment), JSON.stringify(state))
}

export async function newPublicationRoot(): Promise<string> {
  return mkdtemp(join(tmpdir(), "agent-managed-bash-release-cli-"))
}

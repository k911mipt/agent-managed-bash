import { basename } from "node:path"
import { canonicalizeJSON } from "./release-candidate-canonical"
import { digest, isRecord, readJSON as readJSONFile, regularBytes, requireString } from "./release-candidate-primitives"
import type { JSONRecord } from "./release-candidate-primitives"
import {
  validateSpdxDocument as validateSpdxDocumentWithSchema,
  validateSpdxDocuments as validateSpdxDocumentsWithSchema,
  validateSpdxFile as validateSpdxFileWithSchema,
  verifyOfficialSpdxSchema as verifyOfficialSpdxSchemaWithExpectedHash,
} from "./release-candidate-spdx"

export { canonicalizeJSON } from "./release-candidate-canonical"; export { assertTrustedContext, isHash, isPositiveID } from "./release-candidate-identity"; export { digest, isRecord, regularBytes, requireString } from "./release-candidate-primitives"; export { parseStrictJSON } from "./release-candidate-json"

export const MAX_SPDX_BYTES = 32 * 1024 * 1024
export const OFFICIAL_SPDX_SCHEMA_SHA256 = "239208b7ac287b3cf5d9a9af23f9d69863971102a5e1587a27a398b43490b89b"

export type ProducerReceipt = {
  readonly commit: string
  readonly name: string
  readonly sha256: string
  readonly size: number
  readonly version: string
}

export type RelationReceipt = {
  readonly predicate_media_type: "application/spdx+json"
  readonly predicate_name: string
  readonly predicate_sha256: string
  readonly subject_name: string
  readonly subject_sha256: string
  readonly subject_size: number
}

export type TrustedReleaseContext = {
  readonly commit: string
  readonly repository: string
  readonly runAttempt: string
  readonly runId: string
  readonly tag: string
  readonly version: string
  readonly workflow: string
  readonly workflowBlob: string
}

const commitPattern = /^[a-f0-9]{40}$/
const hashPattern = /^[a-f0-9]{64}$/
const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/

export const primaryNames = (version: string): readonly string[] => [
  `agent-managed-bash-${version}-linux-amd64.tar.gz`,
  `agent-managed-bash-${version}-linux-arm64.tar.gz`,
  `agent-managed-bash-${version}-darwin-amd64.tar.gz`,
  `agent-managed-bash-${version}-darwin-arm64.tar.gz`,
  `k911mipt-opencode-agent-managed-bash-${version}.tgz`,
  "install-release.sh",
]

export const scannedNames = (version: string): readonly string[] => primaryNames(version).slice(0, 5)

export async function readJSON(path: string, maximum = MAX_SPDX_BYTES): Promise<unknown> {
  return readJSONFile(path, maximum)
}

function requireNumber(value: unknown, field: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
    throw new Error(`invalid ${field}`)
  }
  return value
}

export function exactKeys(value: JSONRecord, keys: readonly string[]): void {
  if (Object.keys(value).length !== keys.length || keys.some((key) => !(key in value))) {
    throw new Error("unexpected receipt fields")
  }
}

export function validateProducerReceipt(value: unknown): ProducerReceipt {
  if (!isRecord(value)) {
    throw new Error("invalid producer receipt")
  }
  exactKeys(value, ["name", "size", "sha256", "version", "commit"])
  const receipt = {
    commit: requireString(value["commit"], "commit"),
    name: requireString(value["name"], "name"),
    sha256: requireString(value["sha256"], "sha256"),
    size: requireNumber(value["size"], "size"),
    version: requireString(value["version"], "version"),
  }
  if (!hashPattern.test(receipt.sha256) || !versionPattern.test(receipt.version) || !commitPattern.test(receipt.commit)) {
    throw new Error("invalid producer receipt identity")
  }
  return receipt
}

export async function createProducerReceipt(request: {
  readonly assetPath: string
  readonly commit: string
  readonly name: string
  readonly version: string
}): Promise<ProducerReceipt> {
  const bytes = await regularBytes(request.assetPath)
  return validateProducerReceipt({
    commit: request.commit,
    name: request.name,
    sha256: digest(bytes),
    size: bytes.byteLength,
    version: request.version,
  })
}

export async function verifyProducerReceipt(request: {
  readonly assetPath: string
  readonly receipt: ProducerReceipt
}): Promise<void> {
  const actual = await createProducerReceipt({
    assetPath: request.assetPath,
    commit: request.receipt.commit,
    name: request.receipt.name,
    version: request.receipt.version,
  })
  if (canonicalizeJSON(actual) !== canonicalizeJSON(request.receipt)) {
    throw new Error("producer receipt sha256 or size mismatch")
  }
}

export async function verifyOfficialSpdxSchema(path: string): Promise<void> {
  await verifyOfficialSpdxSchemaWithExpectedHash(path, OFFICIAL_SPDX_SCHEMA_SHA256)
}

export async function validateSpdxDocument(document: unknown, schemaPath: string): Promise<void> {
  await validateSpdxDocumentWithSchema(document, schemaPath, OFFICIAL_SPDX_SCHEMA_SHA256)
}

export async function validateSpdxFile(path: string, schemaPath: string): Promise<void> {
  await validateSpdxFileWithSchema(path, schemaPath, OFFICIAL_SPDX_SCHEMA_SHA256)
}

export async function validateSpdxDocuments(documents: readonly unknown[], schemaPath: string): Promise<void> {
  await validateSpdxDocumentsWithSchema(documents, schemaPath, OFFICIAL_SPDX_SCHEMA_SHA256)
}

export function validateRelationReceipt(value: unknown): RelationReceipt {
  if (!isRecord(value)) {
    throw new Error("invalid relation receipt")
  }
  exactKeys(value, ["subject_name", "subject_sha256", "subject_size", "predicate_name", "predicate_sha256", "predicate_media_type"])
  const mediaType = requireString(value["predicate_media_type"], "predicate media type")
  const receipt = {
    predicate_name: requireString(value["predicate_name"], "predicate name"),
    predicate_sha256: requireString(value["predicate_sha256"], "predicate sha256"),
    subject_name: requireString(value["subject_name"], "subject name"),
    subject_sha256: requireString(value["subject_sha256"], "subject sha256"),
    subject_size: requireNumber(value["subject_size"], "subject size"),
  }
  if (!hashPattern.test(receipt.subject_sha256) || !hashPattern.test(receipt.predicate_sha256) || mediaType !== "application/spdx+json") {
    throw new Error("invalid relation receipt hash")
  }
  return { ...receipt, predicate_media_type: "application/spdx+json" }
}

export async function createRelationReceipt(request: {
  readonly assetPath: string
  readonly predicatePath: string
  readonly schemaPath: string
  readonly subject: ProducerReceipt
}): Promise<RelationReceipt> {
  await verifyProducerReceipt({ assetPath: request.assetPath, receipt: request.subject })
  const document = await readJSON(request.predicatePath)
  await validateSpdxDocument(document, request.schemaPath)
  if (!isRecord(document) || document["name"] !== request.subject.name) {
    throw new Error("SBOM subject identity mismatch")
  }
  return validateRelationReceipt({
    predicate_media_type: "application/spdx+json",
    predicate_name: basename(request.predicatePath),
    predicate_sha256: digest(canonicalizeJSON(document)),
    subject_name: request.subject.name,
    subject_sha256: request.subject.sha256,
    subject_size: request.subject.size,
  })
}

export async function verifyRelationReceipt(request: {
  readonly assetPath: string
  readonly predicatePath: string
  readonly relation: RelationReceipt
  readonly schemaPath: string
  readonly subject: ProducerReceipt
}): Promise<void> {
  const actual = await createRelationReceipt(request)
  if (canonicalizeJSON(actual) !== canonicalizeJSON(request.relation)) {
    throw new Error("relation receipt identity mismatch")
  }
}

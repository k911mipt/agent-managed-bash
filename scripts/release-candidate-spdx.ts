import Ajv from "ajv"
import { canonicalizeJSON } from "./release-candidate-canonical"
import { digest, isRecord, readJSON, regularBytes } from "./release-candidate-primitives"

type JSONRecord = Record<string, unknown>

export async function verifyOfficialSpdxSchema(path: string, expectedSha256: string): Promise<void> {
  if (digest(await regularBytes(path)) !== expectedSha256) {
    throw new Error("official SPDX schema sha256 mismatch")
  }
}

export async function validateSpdxDocument(document: unknown, schemaPath: string, expectedSchemaSha256: string): Promise<void> {
  await verifyOfficialSpdxSchema(schemaPath, expectedSchemaSha256)
  const schema = await readJSON(schemaPath)
  if (!isRecord(schema) || schema["$schema"] !== "http://json-schema.org/draft-07/schema#") {
    throw new Error("unsupported SPDX schema dialect")
  }
  const validate = new Ajv({ allErrors: true, strict: true }).compile(schema)
  if (!validate(document)) {
    throw new Error(`SPDX schema validation failed: ${canonicalizeJSON(validate.errors)}`)
  }
  if (!isRecord(document) || document["spdxVersion"] !== "SPDX-2.3" || document["dataLicense"] !== "CC0-1.0" || document["SPDXID"] !== "SPDXRef-DOCUMENT") {
    throw new Error("invalid SPDX document identity")
  }
  const info = document["creationInfo"]
  if (!isRecord(info) || !Array.isArray(info["creators"]) || info["creators"].length === 0 || info["creators"].some((creator) => typeof creator !== "string" || creator.length === 0)) {
    throw new Error("invalid SPDX creators")
  }
  const described = Array.isArray(document["documentDescribes"]) ? document["documentDescribes"] : []
  const packages = Array.isArray(document["packages"]) ? document["packages"] : []
  const files = Array.isArray(document["files"]) ? document["files"] : []
  if (packages.length + files.length === 0) {
    throw new Error("empty SPDX inventory")
  }
  const entries = [...packages, ...files].filter(isRecord)
  const ids = new Set([document["SPDXID"], ...entries.map((entry) => entry["SPDXID"]).filter((id): id is string => typeof id === "string")])
  if (!described.every((id) => typeof id === "string" && ids.has(id) && id !== "SPDXRef-DOCUMENT")) {
    throw new Error("described SPDX target must be package or file")
  }
  const entryByID = new Map<string, JSONRecord>()
  for (const entry of entries) {
    const entryID = entry["SPDXID"]
    if (typeof entryID === "string") {
      entryByID.set(entryID, entry)
    }
  }
  const describedEntries = described.map((id) => entryByID.get(id))
  const nonempty = (value: unknown): value is string => typeof value === "string" && value.trim().length > 0
  const hasChecksum = (value: unknown): boolean => Array.isArray(value) && value.some((checksum) => isRecord(checksum) && nonempty(checksum["algorithm"]) && nonempty(checksum["checksumValue"]))
  const hasExternalReference = (value: unknown): boolean => Array.isArray(value) && value.some((reference) => isRecord(reference) && nonempty(reference["referenceType"]) && nonempty(reference["referenceLocator"]))
  const meaningful = describedEntries.some((entry) => {
    if (entry === undefined || !nonempty(entry["SPDXID"])) {
      return false
    }
    if (packages.includes(entry)) {
      return nonempty(entry["name"]) && (nonempty(entry["versionInfo"]) || hasChecksum(entry["checksums"]) || hasExternalReference(entry["externalRefs"]))
    }
    return nonempty(entry["fileName"]) && hasChecksum(entry["checksums"])
  })
  if (!meaningful) {
    throw new Error("described SPDX inventory lacks identity")
  }
  for (const relationship of Array.isArray(document["relationships"]) ? document["relationships"] : []) {
    if (!isRecord(relationship) || !ids.has(requireString(relationship["spdxElementId"], "relationship endpoint")) || !ids.has(requireString(relationship["relatedSpdxElement"], "relationship endpoint"))) {
      throw new Error("invalid SPDX relationship")
    }
  }
}

export async function validateSpdxFile(path: string, schemaPath: string, expectedSchemaSha256: string): Promise<void> {
  await validateSpdxDocument(await readJSON(path, 32 * 1024 * 1024), schemaPath, expectedSchemaSha256)
}

export async function validateSpdxDocuments(documents: readonly unknown[], schemaPath: string, expectedSchemaSha256: string): Promise<void> {
  const namespaces = new Set<string>()
  for (const document of documents) {
    await validateSpdxDocument(document, schemaPath, expectedSchemaSha256)
    if (!isRecord(document) || typeof document["documentNamespace"] !== "string" || namespaces.has(document["documentNamespace"])) {
      throw new Error("duplicate SPDX namespace")
    }
    namespaces.add(document["documentNamespace"])
  }
}

function requireString(value: unknown, field: string): string {
  if (typeof value !== "string") {
    throw new Error(`invalid ${field}`)
  }
  return value
}

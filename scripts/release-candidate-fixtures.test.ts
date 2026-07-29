import { createHash } from "node:crypto"
import { mkdir, writeFile } from "node:fs/promises"
import { join } from "node:path"

export const fixtureVersion = "0.1.0"
export const fixtureCommit = "0123456789abcdef0123456789abcdef01234567"

export type FixtureJSON =
  | null
  | boolean
  | number
  | string
  | readonly FixtureJSON[]
  | { readonly [key: string]: FixtureJSON }

export const primaryAssetNames = [
  "agent-managed-bash-0.1.0-linux-amd64.tar.gz",
  "agent-managed-bash-0.1.0-linux-arm64.tar.gz",
  "agent-managed-bash-0.1.0-darwin-amd64.tar.gz",
  "agent-managed-bash-0.1.0-darwin-arm64.tar.gz",
  "k911mipt-opencode-agent-managed-bash-0.1.0.tgz",
  "install-release.sh",
] as const

export const scannedAssetNames = primaryAssetNames.slice(0, 5)

export function sha256(value: string | Uint8Array): string {
  return createHash("sha256").update(value).digest("hex")
}

export function canonicalizeFixture(value: FixtureJSON): string {
  if (value === null || typeof value === "boolean" || typeof value === "number" || typeof value === "string") {
    return JSON.stringify(value)
  }
  if (Array.isArray(value)) {
    return `[${value.map(canonicalizeFixture).join(",")}]`
  }
  if (isFixtureRecord(value)) {
    return `{${Object.keys(value)
      .sort()
      .map((key) => { const child = value[key]; if (child === undefined) throw new Error("fixture key is missing"); return `${JSON.stringify(key)}:${canonicalizeFixture(child)}` })
      .join(",")}}`
  }
  throw new Error("fixture value is not JSON")
}

function isFixtureRecord(value: FixtureJSON): value is { readonly [key: string]: FixtureJSON } {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

export function validSpdxDocument(subjectName: string = "agent-managed-bash-0.1.0-linux-amd64.tar.gz"): { readonly [key: string]: FixtureJSON } {
  return {
    SPDXID: "SPDXRef-DOCUMENT",
    creationInfo: {
      created: "2026-07-27T00:00:00Z",
      creators: ["Tool: task-5-fixture"],
    },
    dataLicense: "CC0-1.0",
    documentDescribes: ["SPDXRef-Package"],
    documentNamespace: `https://example.test/spdx/${subjectName}`,
    name: subjectName,
    packages: [
      {
        SPDXID: "SPDXRef-Package",
        checksums: [{ algorithm: "SHA256", checksumValue: "0".repeat(64) }],
        downloadLocation: "NOASSERTION",
        name: subjectName,
        versionInfo: fixtureVersion,
      },
    ],
    relationships: [
      {
        relatedSpdxElement: "SPDXRef-Package",
        relationshipType: "DESCRIBES",
        spdxElementId: "SPDXRef-DOCUMENT",
      },
    ],
    spdxVersion: "SPDX-2.3",
  }
}

export async function writeCandidateFixture(root: string): Promise<void> {
  const producers = join(root, "producers")
  const relations = join(root, "relations")
  await Promise.all([mkdir(producers, { recursive: true }), mkdir(relations, { recursive: true })])

  for (const name of primaryAssetNames) {
    const data = `fixture asset ${name}\n`
    await writeFile(join(producers, name), data)
    await writeFile(
      join(producers, `${name}.receipt.json`),
      `${canonicalizeFixture({
        commit: fixtureCommit,
        name,
        sha256: sha256(data),
        size: Buffer.byteLength(data),
        version: fixtureVersion,
      })}\n`,
    )
  }

  for (const name of scannedAssetNames) {
    const predicateName = `${name}.spdx.json`
    const spdx = validSpdxDocument(name)
    const predicate = `${JSON.stringify(spdx)}\n`
    const subject = {
      commit: fixtureCommit,
      name,
      sha256: sha256(`fixture asset ${name}\n`),
      size: Buffer.byteLength(`fixture asset ${name}\n`),
      version: fixtureVersion,
    }
    await writeFile(join(relations, predicateName), predicate)
    await writeFile(
      join(relations, `${predicateName}.relation.json`),
      `${canonicalizeFixture({
        predicate_media_type: "application/spdx+json",
        predicate_name: predicateName,
        predicate_sha256: sha256(canonicalizeFixture(spdx)),
        subject_name: subject.name,
        subject_sha256: subject.sha256,
        subject_size: subject.size,
      })}\n`,
    )
  }
}

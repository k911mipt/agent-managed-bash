import { readdir } from "node:fs/promises"
import { join } from "node:path"
import { canonicalizeJSON, digest, primaryNames, regularBytes, scannedNames } from "./release-candidate-data"

export async function candidateManifest(path: string): Promise<{ readonly manifest: string; readonly manifest_sha256: string }> {
  const manifest = new TextDecoder().decode(await regularBytes(join(path, "SHA256SUMS")))
  return { manifest, manifest_sha256: digest(manifest) }
}

export async function validateReleaseCandidate(path: string): Promise<void> {
  const manifest = await candidateManifest(path)
  const lines = manifest.manifest.trim().split("\n")
  const entries = lines.map((line) => /^([a-f0-9]{64})  ([^/]+)$/.exec(line))
  if (entries.some((entry) => entry?.[1] === undefined || entry[2] === undefined)) {
    throw new Error("invalid candidate checksum")
  }
  const names = entries.map((entry) => entry?.[2] ?? "")
  const hashes = entries.map((entry) => entry?.[1] ?? "")
  const native = names.find((name) => /^agent-managed-bash-(\d+\.\d+\.\d+)-linux-amd64\.tar\.gz$/.test(name))
  const version = native?.match(/^agent-managed-bash-(\d+\.\d+\.\d+)-/)?.[1]
  const expected = version === undefined ? [] : [...primaryNames(version), ...scannedNames(version).map((name) => `${name}.spdx.json`)].sort()
  if (version === undefined || names.length !== expected.length || new Set(names).size !== names.length || new Set(hashes).size !== hashes.length || canonicalizeJSON([...names].sort()) !== canonicalizeJSON(expected) || canonicalizeJSON((await readdir(path)).sort()) !== canonicalizeJSON([...expected, "SHA256SUMS"].sort())) {
    throw new Error("unexpected candidate files")
  }
  await Promise.all(entries.map(async (entry) => {
    const hash = entry?.[1]
    const name = entry?.[2]
    if (hash === undefined || name === undefined || digest(await regularBytes(join(path, name))) !== hash) {
      throw new Error("invalid or duplicate checksum")
    }
  }))
}

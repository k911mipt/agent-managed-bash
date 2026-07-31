import { describe, expect, test } from "bun:test"
import { createHash } from "node:crypto"
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { releasePublication } from "./release-publish"
import { addExactAttestations, fakeStatePath, runPublication, setupPublication } from "./release-publish-cli-fixtures"

describe("release publication", () => {
  test("stages only the candidate tarball and leaves a complete draft when npm is absent", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-publish-"))
    const tarballBytes = "candidate bytes\n"
    const assets = await Promise.all(Array.from({ length: 12 }, async (_, index) => {
      const name = `candidate-${index}`
      const path = join(root, name)
      await writeFile(path, tarballBytes)
      return { name, path, sha256: createHash("sha256").update(tarballBytes).digest("hex") }
    }))

    try {
      // When
      const result = await releasePublication({
        candidate: {
          assets,
          commit: "0123456789abcdef0123456789abcdef01234567",
          repository: "k911mipt/agent-managed-bash",
          tag: "v0.1.0",
          version: "0.1.0",
          workflowBlob: "0123456789abcdef0123456789abcdef01234567",
        },
        mode: "stage",
      })

      // Then
      expect(result.kind).toBe("staged")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("fails closed when a non-404 attestation error contains not found", async () => {
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-api-error-"))
    const environment = await setupPublication(root)
    await writeFile(fakeStatePath(environment), JSON.stringify({ attestationAPIError: "repository not found after authentication failure" }))

    try {
      const result = await runPublication(["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)

      expect(result.exitCode).not.toBe(0)
      expect(result.stderr).toContain("attestation enumeration failed")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("ignores an unrelated release attestation for a reused asset", async () => {
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-reused-asset-"))
    const environment = await setupPublication(root)
    const arguments_ = ["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]

    try {
      expect((await runPublication(arguments_, environment)).exitCode).toBe(0)
      const state = JSON.parse(await Bun.file(fakeStatePath(environment)).text())
      const asset = state.release.assets[0]
      const payload = {
        _type: "https://in-toto.io/Statement/v1",
        predicate: {},
        predicateType: "https://in-toto.io/attestation/release/v0.2",
        subject: [{ digest: { sha1: "a".repeat(40) }, uri: "pkg:github/k911mipt/agent-managed-bash@v0.1.0" }, { digest: { sha256: asset.digest.replace("sha256:", "") }, name: asset.name }],
      }
      const bundle = { dsseEnvelope: { payload: Buffer.from(JSON.stringify(payload)).toString("base64"), payloadType: "application/vnd.in-toto+json", signatures: [{ keyid: "test", sig: "verified" }] } }
      state.attestations = { [asset.digest.replace("sha256:", "")]: [{ bundle }] }
      await writeFile(fakeStatePath(environment), JSON.stringify(state))

      const result = await runPublication(arguments_, environment)

      expect(result.stderr).toBe("")
      expect(result.exitCode).toBe(0)
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })

  test("uses current provenance when a reused asset has historical SLSA provenance", async () => {
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-historical-provenance-"))
    const environment = await setupPublication(root)
    const stageArguments = ["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]

    try {
      expect((await runPublication(stageArguments, environment)).exitCode).toBe(0)
      await addExactAttestations(root, environment)
      let state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
      const digest = Object.keys(state.attestations)[0]
      if (digest === undefined) throw new Error("missing attestation fixture")
      const oldBundle = structuredClone(state.attestations[digest][0].bundle)
      const payload = JSON.parse(Buffer.from(oldBundle.dsseEnvelope.payload, "base64").toString())
      payload.predicate.buildDefinition.externalParameters.workflow.ref = "refs/tags/v0.1.4"
      payload.predicate.buildDefinition.resolvedDependencies[0].digest.gitCommit = "f".repeat(40)
      payload.predicate.runDetails.builder.id = "https://github.com/k911mipt/agent-managed-bash/.github/workflows/release.yml@refs/tags/v0.1.4"
      oldBundle.dsseEnvelope.payload = Buffer.from(JSON.stringify(payload)).toString("base64")
      state.attestations = { [digest]: [{ bundle: oldBundle }, { bundle: oldBundle }] }
      state.historicalAttestationBundle = oldBundle
      await writeFile(fakeStatePath(environment), JSON.stringify(state))

      expect((await runPublication(stageArguments, environment)).exitCode).toBe(0)
      await addExactAttestations(root, environment)
      state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
      state.attestations[digest].unshift({ bundle: oldBundle }, { bundle: oldBundle })
      await writeFile(fakeStatePath(environment), JSON.stringify(state))
      const finalized = await runPublication(["finalize", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)
      const children = await Promise.all((await readdir(root)).filter((name) => name.startsWith("state.json.child-")).map(async (name) => JSON.parse(await readFile(join(root, name), "utf8"))))
      const verifier = children.find((child) => child.kind === "gh" && child.arguments_[0] === "attestation" && !child.arguments_.includes("--format") && child.arguments_.includes("https://slsa.dev/provenance/v1"))
      const download = children.find((child) => child.kind === "gh" && child.arguments_[0] === "attestation" && child.arguments_[1] === "download")

      expect(finalized.exitCode).toBe(0)
      expect(download.arguments_[download.arguments_.indexOf("--limit") + 1]).toBe("1000")
      expect(download.arguments_[download.arguments_.indexOf("--repo") + 1]).toBe("k911mipt/agent-managed-bash")
      expect(verifier.arguments_).toContain("--deny-self-hosted-runners")
      expect(verifier.arguments_[verifier.arguments_.indexOf("--repo") + 1]).toBe("k911mipt/agent-managed-bash")
      expect(verifier.arguments_[verifier.arguments_.indexOf("--predicate-type") + 1]).toBe("https://slsa.dev/provenance/v1")
      expect(verifier.arguments_[verifier.arguments_.indexOf("--cert-identity") + 1]).toBe("https://github.com/k911mipt/agent-managed-bash/.github/workflows/release.yml@refs/tags/v0.1.0")
      expect(verifier.arguments_[verifier.arguments_.indexOf("--cert-oidc-issuer") + 1]).toBe("https://token.actions.githubusercontent.com")
      expect(verifier.arguments_[verifier.arguments_.indexOf("--signer-digest") + 1]).toBe("0123456789abcdef0123456789abcdef01234567")
      expect(verifier.arguments_[verifier.arguments_.indexOf("--source-ref") + 1]).toBe("refs/tags/v0.1.0")
      expect(verifier.arguments_[verifier.arguments_.indexOf("--source-digest") + 1]).toBe("0123456789abcdef0123456789abcdef01234567")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  }, { timeout: 20_000 })

  test("fails closed on GitHub verifier operational exits", async () => {
    for (const exitCode of [1, 2, 4]) {
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-verifier-exit-"))
      const environment = await setupPublication(root)
      try {
        const candidate = join(root, "candidate")
        const control = join(root, "control", "CANDIDATE-RECEIPT.json")
        expect((await runPublication(["stage", "--candidate", candidate, "--control", control], environment)).exitCode).toBe(0)
        await addExactAttestations(root, environment)
        const state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
        state.githubAttestationVerifyExitCode = exitCode
        await writeFile(fakeStatePath(environment), JSON.stringify(state))

        const result = await runPublication(["finalize", "--candidate", candidate, "--control", control], environment)

        expect(result.exitCode).not.toBe(0)
        expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8")).release.isDraft).toBeTrue()
      } finally {
        await rm(root, { force: true, recursive: true })
      }
    }
  }, { timeout: 30_000 })
})

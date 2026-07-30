import { describe, expect, test } from "bun:test"
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { fixtureVersion } from "./release-candidate-fixtures.test"
import { addExactAttestations, fakeStatePath, runPublication, setupPublication } from "./release-publish-cli-fixtures"

describe("release publication credential and verifier surface", () => {
  test("removes every verifier bundle root after success and verifier failure", async () => {
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-attestation-cleanup-"))
    const temporaryDirectory = join(root, "tmp")
    await mkdir(temporaryDirectory)
    const environment = { ...(await setupPublication(root)), TMPDIR: temporaryDirectory }
    const arguments_ = ["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]

    try {
      expect((await runPublication(arguments_, environment)).exitCode).toBe(0)
      expect((await readdir(temporaryDirectory)).filter((name) => name.startsWith("agent-managed-bash-attestation-")).length).toBe(0)
      const state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
      state.attestationVerifyExitCode = 1
      await writeFile(fakeStatePath(environment), JSON.stringify(state))
      await addExactAttestations(root, environment)
      expect((await runPublication(["finalize", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)).exitCode).not.toBe(0)
      expect((await readdir(temporaryDirectory)).filter((name) => name.startsWith("agent-managed-bash-attestation-")).length).toBe(0)
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("rejects stale bootstrap credentials before public state changes", async () => {
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-token-"))
    const environment = await setupPublication(root)

    try {
      const result = await runPublication(["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], { ...environment, NPM_TOKEN: "stale" })

      expect(result.exitCode).not.toBe(0)
      expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["npm"]).toBeUndefined()
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("rejects stale bootstrap version before public state changes", async () => {
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-bootstrap-version-"))
    const environment = await setupPublication(root)

    try {
      const result = await runPublication(["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], { ...environment, NPM_BOOTSTRAP_VERSION: fixtureVersion })

      expect(result.exitCode).not.toBe(0)
      expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["npm"]).toBeUndefined()
      expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["release"]).toBeUndefined()
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("uses an exact bootstrap token only for the absent-package publish child", async () => {
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-bootstrap-"))
    const environment = await setupPublication(root)
    const arguments_ = ["stage", "--candidate", "candidate", "--control", join("control", "CANDIDATE-RECEIPT.json")]

    try {
      const result = await runPublication(arguments_, { ...environment, NPM_BOOTSTRAP_VERSION: fixtureVersion, NPM_TOKEN: "bootstrap-token", RELEASE_FIRST_PUBLISH_BOOTSTRAP: "true" }, root)
      const children = await Promise.all((await readdir(root)).filter((name) => name.startsWith("state.json.child-")).map(async (name) => JSON.parse(await readFile(join(root, name), "utf8"))))
      const tokenChildren = children.filter((child: { readonly hasNodeAuthToken: boolean }) => child.hasNodeAuthToken)

      expect(result.exitCode).toBe(0)
      expect(tokenChildren).toEqual([{ arguments_: ["publish", expect.stringMatching(/^\/.*\.tgz$/), "--access", "public", "--provenance"], hasNodeAuthToken: true, kind: "npm" }])
      expect(children.every((child: { readonly arguments_: readonly string[] }) => child.arguments_.every((argument) => argument !== "bootstrap-token"))).toBeTrue()
      expect(JSON.stringify(children)).not.toContain("bootstrap-token")
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("reports mutation stderr without exposing the bootstrap token", async () => {
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-publish-error-"))
    const environment = await setupPublication(root)
    await writeFile(fakeStatePath(environment), JSON.stringify({ npmPublishExitCode: 1, npmPublishStderr: "publish rejected bootstrap-token" }))

    try {
      const result = await runPublication(["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], { ...environment, NPM_BOOTSTRAP_VERSION: fixtureVersion, NPM_TOKEN: "bootstrap-token", RELEASE_FIRST_PUBLISH_BOOTSTRAP: "true" })

      expect(result.exitCode).not.toBe(0)
      expect(result.stderr).toContain("publish rejected ***")
      expect(result.stderr).not.toContain("bootstrap-token")
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("rejects malformed and stale bootstrap credentials before child commands", async () => {
    const cases = [
      { NPM_BOOTSTRAP_VERSION: fixtureVersion, RELEASE_FIRST_PUBLISH_BOOTSTRAP: "true" },
      { NPM_TOKEN: "bootstrap-token", RELEASE_FIRST_PUBLISH_BOOTSTRAP: "true" },
      { NPM_BOOTSTRAP_VERSION: "0.0.0", NPM_TOKEN: "bootstrap-token", RELEASE_FIRST_PUBLISH_BOOTSTRAP: "true" },
      { NPM_BOOTSTRAP_VERSION: fixtureVersion, NPM_TOKEN: "bootstrap-token" },
      { NPM_BOOTSTRAP_VERSION: fixtureVersion, NPM_TOKEN: "bootstrap-token", RELEASE_FIRST_PUBLISH_BOOTSTRAP: "wrong" },
    ]

    for (const credentials of cases) {
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-bootstrap-reject-"))
      const environment = await setupPublication(root)
      try {
        const result = await runPublication(["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], { ...environment, ...credentials })
        expect(result.exitCode).not.toBe(0)
        expect((await readdir(root)).some((name) => name.startsWith("state.json.child-"))).toBeFalse()
      } finally { await rm(root, { force: true, recursive: true }) }
    }
  }, { timeout: 20_000 })

  test("requires the exact SPDX predicate type in verified SBOM bundles and verifier arguments", async () => {
    for (const sbomPredicateType of ["https://spdx.dev/Document", "https://spdx.dev/Document/v2.2"]) {
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-spdx-"))
      const environment = await setupPublication(root)
      try {
        expect((await runPublication(["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)).exitCode).toBe(0)
        const state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
        state.sbomPredicateType = sbomPredicateType
        await writeFile(fakeStatePath(environment), JSON.stringify(state))
        await addExactAttestations(root, environment)
        expect((await runPublication(["finalize", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)).exitCode).not.toBe(0)
      } finally { await rm(root, { force: true, recursive: true }) }
    }
  }, { timeout: 20_000 })
})

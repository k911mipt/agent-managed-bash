import { describe, expect, test } from "bun:test"
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { fixtureCommit, fixtureVersion } from "./release-candidate-fixtures.test"
import { addExactAttestations, fakeStatePath, runPublication, setupPublication } from "./release-publish-cli-fixtures"

describe("release publication fake CLI surface", () => {
  test("stages, resumes, and finalizes exact candidate bytes", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-cli-"))
    const environment = await setupPublication(root)
    const arguments_ = ["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]

    try {
      // When
       const staged = await runPublication(arguments_, environment)
       expect(staged.exitCode).toBe(0)
       const resumed = await runPublication(arguments_, environment)
       await addExactAttestations(root, environment)
       const finalized = await runPublication(["finalize", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)

      // Then
      expect(staged.exitCode).toBe(0)
      expect(resumed.exitCode).toBe(0)
      expect(finalized.exitCode).toBe(0)
      expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["release"]["isImmutable"]).toBeTrue()
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("refuses a matching SRI package with a wrong provenance workflow before a draft exists", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-provenance-"))
    const environment = await setupPublication(root)
    await writeFile(fakeStatePath(environment), JSON.stringify({ npm: { name: "@k911mipt/opencode-agent-managed-bash", version: fixtureVersion, dist: { integrity: "sha512-wrong" }, provenance: { repository: "k911mipt/agent-managed-bash", workflow: "wrong.yml", commit: fixtureCommit, subject_sha256: "0".repeat(64) } } }))

    try {
      // When
      const result = await runPublication(["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)

      // Then
      expect(result.exitCode).not.toBe(0)
      expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["release"]).toBeUndefined()
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("accepts only the original receipted run, tag, SHA, and workflow blob for recovery", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-recovery-"))
    const environment = await setupPublication(root)
    const arguments_ = ["recovery", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]

    try {
      // When
      const matching = await runPublication(arguments_, environment)
      const wrongRun = await runPublication(arguments_, { ...environment, RELEASE_RECOVERY_RUN_ID: "457" })
      const wrongBlob = await runPublication(arguments_, { ...environment, RELEASE_WORKFLOW_BLOB: "fedcba9876543210fedcba9876543210fedcba98" })

      // Then
      expect(matching.exitCode).toBe(0)
      expect(wrongRun.exitCode).not.toBe(0)
      expect(wrongBlob.exitCode).not.toBe(0)
      expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["release"]).toBeUndefined()
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("reconciles a matching partial draft by uploading only missing candidate assets", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-partial-"))
    const environment = await setupPublication(root)
    const arguments_ = ["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]
    expect((await runPublication(arguments_, environment)).exitCode).toBe(0)
    const state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
    state.release.assets = [state.release.assets[0]]
    await writeFile(fakeStatePath(environment), JSON.stringify(state))

    try {
      // When
      const resumed = await runPublication(arguments_, environment)

      // Then
      expect(resumed.exitCode).toBe(0)
      expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["release"]["assets"]).toHaveLength(12)
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("reconciles delayed and ambiguous npm and draft mutations without repeating them", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-ambiguous-"))
    const environment = await setupPublication(root)
    await writeFile(fakeStatePath(environment), JSON.stringify({ ambiguous: "npm-publish", npmDelayOnPublish: 1, releaseDelayOnCreate: 1 }))
    const arguments_ = ["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]

    try {
      // When
      const result = await runPublication(arguments_, environment)

      // Then
      expect(result.exitCode).toBe(0)
      const state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
      expect(state.release.assets).toHaveLength(12)
      expect(state.npmDelay).toBe(0)
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("returns terminal no-op only for a complete immutable receipted release", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-terminal-"))
    const environment = await setupPublication(root)
    const arguments_ = ["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]

    try {
      // When
      expect((await runPublication(arguments_, environment)).exitCode).toBe(0)
      await addExactAttestations(root, environment)
      expect((await runPublication(["finalize", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)).exitCode).toBe(0)
      const terminal = await runPublication(arguments_, environment)

      // Then
      expect(terminal.exitCode).toBe(0)
      expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["release"]["isImmutable"]).toBeTrue()
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  test("rejects duplicate and swapped verified DSSE envelopes before finalization", async () => {
    // Given
    const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-dsse-"))
    const environment = await setupPublication(root)
    const arguments_ = ["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]

    try {
      expect((await runPublication(arguments_, environment)).exitCode).toBe(0)
      await addExactAttestations(root, environment)
      const state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
      const digest = Object.keys(state.attestations)[0]
      if (digest === undefined) throw new Error("missing attestation fixture")
      state.attestations[digest].push(state.attestations[digest][0])
      await writeFile(fakeStatePath(environment), JSON.stringify(state))

      // When
      const result = await runPublication(["finalize", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)

      // Then
      expect(result.exitCode).not.toBe(0)
      expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["release"]["isDraft"]).toBeTrue()
    } finally { await rm(root, { force: true, recursive: true }) }
  })

  for (const testCase of [
    { githubClaims: { path: ".github/workflows/wrong.yml" }, name: "workflow" },
    { githubClaims: { ref: "refs/heads/master" }, name: "ref" },
    { githubClaims: { sha: "f".repeat(40) }, name: "sha" },
    { githubClaims: { environment: "self-hosted" }, name: "environment" },
    { githubClaims: { builder: "https://github.com/actions/runner/github-hosted" }, name: "hosted runner builder" },
    { githubClaims: { builder: "https://github.com/npm/cli/.github/workflows/release.yml@refs/tags/v0.1.0" }, name: "npm builder" },
    { githubClaims: { builder: "https://github.com/k911mipt/agent-managed-bash/.github/workflows/wrong.yml@refs/tags/v0.1.0" }, name: "wrong workflow builder" },
    { githubSubjectShape: "missing", name: "missing subject" },
    { githubSubjectShape: "extra", name: "extra subject" },
    { githubSubjectShape: "duplicate", name: "duplicate subject" },
  ]) {
    test(`rejects real-shaped provenance conflict: ${testCase.name}`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), `agent-managed-bash-release-${testCase.name}-`))
      const environment = await setupPublication(root)
      const arguments_ = ["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")]

      try {
        expect((await runPublication(arguments_, environment)).exitCode).toBe(0)
        const state = JSON.parse(await readFile(fakeStatePath(environment), "utf8"))
        Object.assign(state, testCase)
        await writeFile(fakeStatePath(environment), JSON.stringify(state))
        await addExactAttestations(root, environment)

        // When
        const result = await runPublication(["finalize", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)

        // Then
        expect(result.exitCode).not.toBe(0)
        expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["release"]["isDraft"]).toBeTrue()
      } finally { await rm(root, { force: true, recursive: true }) }
    }, { timeout: 20_000 })
  }

  for (const testCase of [
    { name: "nonempty invalid", npmInvalid: [{ name: "candidate" }] },
    { name: "nonempty missing", npmMissing: [{ name: "candidate" }] },
    { name: "audit nonzero", npmAuditExitCode: 1 },
    { githubClaims: { sha: "f".repeat(40) }, name: "swapped provenance bundle" },
    { name: "encoded scoped slash", npmPurl: `pkg:npm/%40k911mipt%2fopencode-agent-managed-bash@${fixtureVersion}` },
    { name: "workflow builder", npmBuilder: `https://github.com/k911mipt/agent-managed-bash/.github/workflows/release.yml@refs/tags/v${fixtureVersion}` },
    { name: "wrong hosted builder", npmBuilder: "https://github.com/actions/runner/self-hosted" },
  ]) {
    test(`rejects npm publication conflict: ${testCase.name}`, async () => {
      // Given
      const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-release-npm-"))
      const environment = await setupPublication(root)
      await writeFile(fakeStatePath(environment), JSON.stringify(testCase))

      try {
        // When
        const result = await runPublication(["stage", "--candidate", join(root, "candidate"), "--control", join(root, "control", "CANDIDATE-RECEIPT.json")], environment)

        // Then
        expect(result.exitCode).not.toBe(0)
        expect(JSON.parse(await readFile(fakeStatePath(environment), "utf8"))["release"]).toBeUndefined()
      } finally { await rm(root, { force: true, recursive: true }) }
    }, { timeout: 20_000 })
  }

})

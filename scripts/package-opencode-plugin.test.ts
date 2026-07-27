import { describe, expect, test } from "bun:test"
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { tmpdir } from "node:os"
import {
  assertPublicPluginStage,
  stagePublicPluginPackage,
} from "./package-opencode-plugin"

const publicPackageManifest = {
  name: "@k911mipt/opencode-agent-managed-bash",
  version: "0.1.0",
  type: "module",
  main: "./managed-bash.js",
  exports: "./managed-bash.js",
  license: "MIT",
  repository: {
    type: "git",
    url: "git+https://github.com/k911mipt/agent-managed-bash.git",
  },
  publishConfig: {
    access: "public",
  },
} as const

describe("public OpenCode plugin package", () => {
  test("stages the approved files and manifest when bundle identity matches VERSION", async () => {
    // Given
    const fixture = await createFixture({ bundleVersion: "0.1.0" })
    const stage = join(fixture.root, "stage")

    try {
      // When
      await stagePublicPluginPackage({ root: fixture.root, stage })

      // Then
      expect((await readdir(stage)).sort()).toEqual([
        "LICENSE",
        "README.md",
        "THIRD_PARTY_NOTICES.txt",
        "managed-bash.js",
        "package.json",
      ])
      expect(await readFile(join(stage, "package.json"), "utf8")).toBe(
        `${JSON.stringify(publicPackageManifest, null, 2)}\n`,
      )
      await expect(assertPublicPluginStage(stage)).resolves.toBeUndefined()
    } finally {
      await rm(fixture.root, { force: true, recursive: true })
    }
  })

  test("rejects staging when bundle identity differs from VERSION", async () => {
    // Given
    const fixture = await createFixture({ bundleVersion: "9.9.9" })

    try {
      // When / Then
      await expect(stagePublicPluginPackage({ root: fixture.root, stage: join(fixture.root, "stage") })).rejects.toThrow(
        'plugin release "9.9.9" does not match VERSION "0.1.0"',
      )
    } finally {
      await rm(fixture.root, { force: true, recursive: true })
    }
  })

  test("rejects staging when VERSION is malformed", async () => {
    // Given
    const fixture = await createFixture({ bundleVersion: "0.1.0", version: "release-0.1.0" })

    try {
      // When / Then
      await expect(stagePublicPluginPackage({ root: fixture.root, stage: join(fixture.root, "stage") })).rejects.toThrow(
        'invalid VERSION: "release-0.1.0"',
      )
    } finally {
      await rm(fixture.root, { force: true, recursive: true })
    }
  })

  test("rejects an unexpected file when validating a staged package", async () => {
    // Given
    const fixture = await createFixture({ bundleVersion: "0.1.0" })
    const stage = join(fixture.root, "stage")

    try {
      await stagePublicPluginPackage({ root: fixture.root, stage })
      await writeFile(join(stage, "unexpected"), "unexpected\n")

      // When / Then
      await expect(assertPublicPluginStage(stage)).rejects.toThrow('unexpected staged package files: unexpected')
    } finally {
      await rm(fixture.root, { force: true, recursive: true })
    }
  })
})

type FixtureRequest = {
  readonly bundleVersion: string
  readonly version?: string
}

type Fixture = {
  readonly root: string
}

async function createFixture(request: FixtureRequest): Promise<Fixture> {
  const root = await mkdtemp(join(tmpdir(), "agent-managed-bash-npm-package-"))
  const bundlePath = join(root, "plugins/opencode/dist/managed-bash.js")

  await mkdir(join(root, "plugins/opencode/dist"), { recursive: true })
  await mkdir(join(root, "packaging"), { recursive: true })
  await Promise.all([
    writeFile(join(root, "VERSION"), `${request.version ?? "0.1.0"}\n`),
    writeFile(join(root, "README.md"), "readme\n"),
    writeFile(join(root, "LICENSE"), "license\n"),
    writeFile(join(root, "packaging/THIRD_PARTY_NOTICES.txt"), "notices\n"),
    writeFile(
      bundlePath,
      [
        "export function ManagedBashPlugin() {}",
        `Object.defineProperty(ManagedBashPlugin, "managedBashReleaseVersion", { value: ${JSON.stringify(request.bundleVersion)} })`,
      ].join("\n"),
    ),
  ])

  return { root }
}

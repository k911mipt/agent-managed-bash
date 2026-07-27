import { copyFile, lstat, mkdir, mkdtemp, readFile, readdir, rename, rm, writeFile } from "node:fs/promises"
import { randomUUID } from "node:crypto"
import { dirname, join, resolve } from "node:path"
import { tmpdir } from "node:os"
import { pathToFileURL } from "node:url"
import {
  createPackInterruption,
  PackageInterruptedError,
} from "./package-opencode-plugin-interruption"
import type { PackInterruption } from "./package-opencode-plugin-interruption"

const packageName = "@k911mipt/opencode-agent-managed-bash"
const packageArchiveName = "k911mipt-opencode-agent-managed-bash"
const approvedPackageFiles = [
  "LICENSE",
  "README.md",
  "THIRD_PARTY_NOTICES.txt",
  "managed-bash.js",
  "package.json",
] as const
const approvedPackageFileSet = new Set<string>(approvedPackageFiles)
const releaseVersionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/
const bundleVersionInspector = `
const bundle = await import(process.argv[1])
const version = bundle.ManagedBashPlugin?.managedBashReleaseVersion
if (typeof version !== "string") process.exit(2)
process.stdout.write(version)
`

export class PublicPackageError extends Error {
  constructor(message: string) {
    super(message)
    this.name = "PublicPackageError"
  }
}

export type StagePublicPluginPackageRequest = {
  readonly root: string
  readonly stage: string
}

export async function stagePublicPluginPackage(request: StagePublicPluginPackageRequest): Promise<void> {
  const root = resolve(request.root)
  const version = await readReleaseVersion(root)
  const bundlePath = join(root, "plugins/opencode/dist/managed-bash.js")

  await assertRegularFile(bundlePath)
  await assertBundleRelease(bundlePath, version)
  await rm(request.stage, { force: true, recursive: true })
  await mkdir(request.stage, { recursive: true })
  await Promise.all([
    copyFile(join(root, "LICENSE"), join(request.stage, "LICENSE")),
    copyFile(join(root, "README.md"), join(request.stage, "README.md")),
    copyFile(join(root, "packaging/THIRD_PARTY_NOTICES.txt"), join(request.stage, "THIRD_PARTY_NOTICES.txt")),
    copyFile(bundlePath, join(request.stage, "managed-bash.js")),
    writeFile(join(request.stage, "package.json"), `${JSON.stringify(publicPackageManifest(version), null, 2)}\n`),
  ])
  await assertPublicPluginStage(request.stage)
}

export async function assertPublicPluginStage(stage: string): Promise<void> {
  const actualFiles = (await readdir(stage)).sort()
  const unexpectedFiles = actualFiles.filter((file) => !approvedPackageFileSet.has(file))
  const missingFiles = approvedPackageFiles.filter((file) => !actualFiles.includes(file))

  if (unexpectedFiles.length > 0) {
    throw new PublicPackageError(`unexpected staged package files: ${unexpectedFiles.join(", ")}`)
  }
  if (missingFiles.length > 0) {
    throw new PublicPackageError(`missing staged package files: ${missingFiles.join(", ")}`)
  }
  await Promise.all(approvedPackageFiles.map((file) => assertRegularFile(join(stage, file))))
}

export async function packagePublicPlugin(rootPath: string): Promise<string> {
  const root = resolve(rootPath)
  const version = await readReleaseVersion(root)
  const output = join(root, "dist/npm", `${packageArchiveName}-${version}.tgz`)
  await mkdir(dirname(output), { recursive: true })
  const stage = await mkdtemp(join(tmpdir(), "agent-managed-bash-npm-package-"))
  const temporaryOutput = join(dirname(output), `.${packageArchiveName}-${version}-${randomUUID()}.tgz`)
  const interruption = createPackInterruption()

  try {
    await stagePublicPluginPackage({ root, stage })
    await packStage(stage, temporaryOutput, interruption)
    await assertRegularFile(temporaryOutput)
    interruption.throwIfInterrupted()
    await rename(temporaryOutput, output)
    return output
  } finally {
    interruption.dispose()
    await Promise.all([
      rm(stage, { force: true, recursive: true }),
      rm(temporaryOutput, { force: true }),
    ])
  }
}

function publicPackageManifest(version: string): Record<string, unknown> {
  return {
    name: packageName,
    version,
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
  }
}

async function readReleaseVersion(root: string): Promise<string> {
  const version = (await readFile(join(root, "VERSION"), "utf8")).trim()
  if (!releaseVersionPattern.test(version)) {
    throw new PublicPackageError(`invalid VERSION: ${JSON.stringify(version)}`)
  }
  return version
}

async function assertBundleRelease(bundlePath: string, version: string): Promise<void> {
  const process = Bun.spawn(["bun", "-e", bundleVersionInspector, pathToFileURL(bundlePath).href], {
    stderr: "pipe",
    stdout: "pipe",
  })
  const [exitCode, stdout, stderr] = await Promise.all([
    process.exited,
    new Response(process.stdout).text(),
    new Response(process.stderr).text(),
  ])
  if (exitCode !== 0) {
    throw new PublicPackageError(`inspect plugin release failed: ${stderr || stdout}`)
  }
  if (stdout !== version) {
    throw new PublicPackageError(`plugin release ${JSON.stringify(stdout)} does not match VERSION ${JSON.stringify(version)}`)
  }
}

async function packStage(stage: string, output: string, interruption: PackInterruption): Promise<void> {
  const subprocess = Bun.spawn(
    ["bun", "pm", "pack", "--ignore-scripts", "--gzip-level", "9", "--filename", output],
    { cwd: stage, detached: true, stderr: "pipe", stdout: "pipe" },
  )
  const result = Promise.all([
    subprocess.exited,
    new Response(subprocess.stdout).text(),
    new Response(subprocess.stderr).text(),
  ])
  interruption.setActivePack({ completed: result.then(() => undefined), subprocess })
  try {
    const [exitCode, stdout, stderr] = await result
    await interruption.waitForTermination()
    interruption.throwIfInterrupted()
    if (exitCode !== 0) {
      throw new PublicPackageError(`pack public plugin failed: ${stderr || stdout}`)
    }
  } finally {
    interruption.setActivePack(undefined)
  }
}

async function assertRegularFile(path: string): Promise<void> {
  const info = await lstat(path)
  if (!info.isFile()) {
    throw new PublicPackageError(`expected regular file: ${path}`)
  }
}

async function main(): Promise<void> {
  try {
    await packagePublicPlugin(process.cwd())
  } catch (error) {
    if (error instanceof PackageInterruptedError) {
      process.exit(error.exitCode)
    }
    throw error
  }
}

if (import.meta.main) {
  await main()
}

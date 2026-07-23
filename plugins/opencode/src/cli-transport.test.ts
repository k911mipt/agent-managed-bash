import { describe, expect, test } from "bun:test"
import { chmod, mkdtemp, rm, symlink, unlink, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import {
  CliProtocolError,
  createBunCliExecutor,
  executeProtocolRequest,
  type CliExecution,
  type CliExecutor,
} from "./cli-transport"
import type { Request, Response } from "./generated/protocol.gen"
import { createResponseValidator } from "./response-validator"

const encoder = new TextEncoder()
const request: Request = { schema_version: 1, action: "version" }
const runRequest: Request = {
  schema_version: 1,
  action: "run",
  context: { session_id: "session-1", workspace_path: "/workspace", cwd: "/workspace" },
  payload: { command: "printf ok" },
}
const abort = new AbortController().signal

describe("managed-bash CLI transport", () => {
  test("accepts one schema-valid newline-delimited response", async () => {
    const response = await executeProtocolRequest(
      executor(result(versionResponse())),
      request,
      abort,
      createResponseValidator(),
    )

    expect(response).toEqual(versionResponse())
  })

  test("rejects missing, malformed, and multi-line CLI stdout", async () => {
    const validate = createResponseValidator()

    for (const stdout of ["", "not json\n", `${JSON.stringify(versionResponse())}\nextra\n`]) {
      await expect(executeProtocolRequest(executor(bytes(stdout)), request, abort, validate)).rejects.toBeInstanceOf(
        CliProtocolError,
      )
    }
  })

  test("rejects a response for a different action or mismatched exit class", async () => {
    const validate = createResponseValidator()
    const wrongAction: Response = {
      schema_version: 1,
      ok: true,
      action: "list",
      result: { jobs: [] },
    }

    await expect(
      executeProtocolRequest(executor(result(wrongAction)), request, abort, validate),
    ).rejects.toBeInstanceOf(CliProtocolError)
    await expect(
      executeProtocolRequest(executor({ ...result(versionResponse()), exitCode: 5 }), request, abort, validate),
    ).rejects.toBeInstanceOf(CliProtocolError)

    const unauthorized: Response = {
      schema_version: 1,
      ok: false,
      action: "run",
      error: { code: "unauthorized", message: "denied" },
    }
    await expect(
      executeProtocolRequest(executor({ ...result(unauthorized), exitCode: 2 }), runRequest, abort, validate),
    ).rejects.toBeInstanceOf(CliProtocolError)
  })

  test("pins a symlinked executable to one physical release", async () => {
    const root = await mkdtemp(join(tmpdir(), "managed-bash-transport-"))
    try {
      const first = join(root, "first")
      const second = join(root, "second")
      const current = join(root, "current")
      await writeVersionScript(first, "1.0.0")
      await writeVersionScript(second, "2.0.0")
      await symlink(first, current)
      const executor = createBunCliExecutor(current)
      const validate = createResponseValidator()

      const before = await executeProtocolRequest(executor, request, abort, validate)
      await unlink(current)
      await symlink(second, current)
      const after = await executeProtocolRequest(executor, request, abort, validate)

      expect(before.ok && before.action === "version" && before.result.binary_version).toBe("1.0.0")
      expect(after.ok && after.action === "version" && after.result.binary_version).toBe("1.0.0")
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  })
})

async function writeVersionScript(path: string, version: string): Promise<void> {
  await writeFile(path, `#!/bin/sh\nread request\nprintf '%s\\n' '${JSON.stringify(versionResponse(version))}'\n`)
  await chmod(path, 0o700)
}

function executor(execution: CliExecution): CliExecutor {
  return { async execute() { return execution } }
}

function bytes(stdout: string): CliExecution {
  return { exitCode: 5, stderr: new Uint8Array(), stdout: encoder.encode(stdout) }
}

function result(value: Response): CliExecution {
  return {
    exitCode: value.ok ? 0 : 5,
    stderr: new Uint8Array(),
    stdout: encoder.encode(`${JSON.stringify(value)}\n`),
  }
}

function versionResponse(binaryVersion = "test"): Extract<Response, { action: "version" }> {
  return {
    schema_version: 1,
    ok: true,
    action: "version",
    result: {
      product: "managed-bash",
      binary_version: binaryVersion,
      protocol_version: 1,
      os: "linux",
      architecture: "amd64",
    },
  }
}

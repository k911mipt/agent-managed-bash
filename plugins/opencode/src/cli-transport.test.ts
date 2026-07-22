import { describe, expect, test } from "bun:test"
import { resolve } from "node:path"
import {
  CliProtocolError,
  executeProtocolRequest,
  type CliExecution,
  type CliExecutor,
} from "./cli-transport"
import type { Request, Response } from "./generated/protocol.gen"
import { createResponseValidator } from "./response-validator"

const encoder = new TextEncoder()
const repositoryRoot = resolve(import.meta.dir, "../../..")
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
      await createResponseValidator(repositoryRoot),
    )

    expect(response).toEqual(versionResponse())
  })

  test("rejects missing, malformed, and multi-line CLI stdout", async () => {
    const validate = await createResponseValidator(repositoryRoot)

    for (const stdout of ["", "not json\n", `${JSON.stringify(versionResponse())}\nextra\n`]) {
      await expect(executeProtocolRequest(executor(bytes(stdout)), request, abort, validate)).rejects.toBeInstanceOf(
        CliProtocolError,
      )
    }
  })

  test("rejects a response for a different action or mismatched exit class", async () => {
    const validate = await createResponseValidator(repositoryRoot)
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
})

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

function versionResponse(): Extract<Response, { action: "version" }> {
  return {
    schema_version: 1,
    ok: true,
    action: "version",
    result: {
      product: "managed-bash",
      binary_version: "test",
      protocol_version: 1,
      os: "linux",
      architecture: "amd64",
    },
  }
}

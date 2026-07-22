# agent-managed-bash

Observable, cancellable long-running shell jobs for coding agents.

> Status: protocol v1, persisted-state policy, the detached per-job Go runner, and the `managed-bash` CLI are implemented. OpenCode integration and packaging remain pending.

The v1 CLI contract permits a bounded one-shot read of the full captured prefix, up to 100 MiB. Capture appends retain only the accepted incoming prefix so repeated writes do not copy the full history.

The OpenCode plugin's 200-line presentation tail is intentionally deferred to issue #6; issue #3 defines only the shared protocol, persistence, and policy contracts it consumes.

## Prerequisites

- Go `1.26.5`
- Bun `1.3.14`
- `make`

The exact Go and Bun versions are pinned in `go.mod` and `.bun-version`.

## Setup

```sh
git clone https://github.com/k911mipt/agent-managed-bash.git
cd agent-managed-bash
make doctor
bun install --frozen-lockfile
```

## Root commands

| Command | Purpose |
|---------|---------|
| `make doctor` | Check that the installed Go and Bun versions match the repository pins. |
| `make schema-generate` | Regenerate the checked-in Go and TypeScript schema DTOs. |
| `make schema-generated-check` | Prove two independent generations are byte-identical and match the checked-in DTOs. |
| `make protocol-schema-test` | Run Go and Ajv draft-2020-12 validation over the shared fixture manifest. |
| `make generated-model-compile` | Compile focused Go and strict TypeScript consumers of the generated DTOs. |
| `make state-schema-test` | Validate immutable policy data and run semantic state-policy conformance tests. |
| `make schema-check` | Run reproducibility, protocol fixtures, generated-model compilation, and state policy. |
| `make runner-test` | Run the detached runner tests with the race detector and randomized order. |
| `make cli-test` | Run focused protocol-validator, CLI, and command-package tests. |
| `make cli-race-test` | Run focused CLI tests with the race detector and randomized order. |
| `make cli-build` | Build `bin/managed-bash`. |
| `make cli-acceptance` | Build and exercise the real binary through help, version, lifecycle, malformed, and incompatible-version scenarios. |
| `make go-check` | Run all Go tests with the race detector, then vet and build every package. |

Run the doctor failure contract with:

```sh
sh tests/doctor_test.sh
sh tests/schema_generation_test.sh
```

## CLI protocol

Build the binary with:

```sh
make cli-build
```

Every action reads one protocol-v1 JSON request from stdin and writes one schema-valid JSON response plus a newline to stdout. Diagnostics use stderr. The public actions are `run`, `wait`, `status`, `output`, `cancel`, `remove`, `list`, and `version`; command text is accepted only inside the `run` request body.

`version` does not require runner initialization or trusted host context:

```sh
printf '%s' '{"schema_version":1,"action":"version"}' | bin/managed-bash version
```

Stateful actions bind the request assertion to host-owned context:

- `MANAGED_BASH_HOST_SESSION_ID`: trusted session identifier.
- `MANAGED_BASH_HOST_WORKSPACE_PATH`: physical canonical workspace root.
- process working directory: trusted current directory, which must be inside that workspace.

The CLI removes both host variables before starting a shell, so commands cannot inherit them. For direct local diagnostics, enter the intended physical cwd, export the two variables, and send matching `context` values in the stdin request. Missing or mismatched host context is `unauthorized`; symlink traversal and paths outside the workspace fail closed.

All successful protocol actions exit 0, including observations of jobs that finish nonzero or by signal. Protocol failures use stable exit classes 2 (validation/version), 3 (authorization/not found), 4 (conflict), and 5 (runner/I/O/internal).

## Repository layout

- `cmd/` and `internal/`: Go commands and internal packages
- `plugins/opencode/`: Bun workspace for the OpenCode plugin
- `schemas/`: shared protocol schemas
- `scripts/`: developer scripts
- `e2e/` and `tests/`: end-to-end and repository checks

## License

[MIT](LICENSE)

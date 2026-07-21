# agent-managed-bash

Observable, cancellable long-running shell jobs for coding agents.

> Status: protocol v1 schemas, generated DTOs, and semantic state policy are implemented. The job runner and integrations are not implemented yet.

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

Run the doctor failure contract with:

```sh
sh tests/doctor_test.sh
sh tests/schema_generation_test.sh
```

## Repository layout

- `cmd/` and `internal/`: Go commands and internal packages
- `plugins/opencode/`: Bun workspace for the OpenCode plugin
- `schemas/`: shared protocol schemas
- `scripts/`: developer scripts
- `e2e/` and `tests/`: end-to-end and repository checks

## License

[MIT](LICENSE)

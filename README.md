# agent-managed-bash

Observable, cancellable long-running shell jobs for coding agents.

> Status: repository bootstrap only. The job runner and integrations are not implemented yet.

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

Run the doctor failure contract with:

```sh
sh tests/doctor_test.sh
```

## Repository layout

- `cmd/` and `internal/`: Go commands and internal packages
- `plugins/opencode/`: Bun workspace for the OpenCode plugin
- `schemas/`: shared protocol schemas
- `scripts/`: developer scripts
- `e2e/` and `tests/`: end-to-end and repository checks

## License

[MIT](LICENSE)

# agent-managed-bash

Observable, cancellable long-running shell jobs for coding agents.

> Status: the protocol, detached runner, CLI, OpenCode plugin, reproducible archives, and per-user installer are implemented.

The v1 CLI contract permits a bounded one-shot read of the full captured prefix, up to 100 MiB. Capture appends retain only the accepted incoming prefix so repeated writes do not copy the full history.

The OpenCode plugin exposes one `managed_bash` tool. It does not fall back to OpenCode's built-in Bash tool when the binary is missing, incompatible, or returns malformed protocol output.

## Prerequisites

- Go `1.26.5`
- Bun `1.3.14`
- `make`
- OpenCode `1.18.x` for plugin discovery and installed E2E

The exact Go and Bun versions are pinned in `go.mod` and `.bun-version`.

## Setup

```sh
git clone https://github.com/k911mipt/agent-managed-bash.git
cd agent-managed-bash
make doctor
bun install --frozen-lockfile
```

## Build and install

Build deterministic archives for Linux and macOS on amd64 and arm64:

```sh
SOURCE_DATE_EPOCH=1700000000 make release-package
```

The command writes four archives under `dist/`. Choose the archive that matches the operating system and architecture reported by `uname -s` and `uname -m`, then run its installer:

```sh
tar -xzf dist/agent-managed-bash-0.1.0-linux-amd64.tar.gz
cd agent-managed-bash-0.1.0-linux-amd64
./install.sh
```

The installer uses these per-user paths:

| Artifact | Default path |
|----------|--------------|
| Releases and `current` pointer | `${XDG_DATA_HOME:-$HOME/.local/share}/agent-managed-bash/` |
| CLI registration | `${MANAGED_BASH_BIN_DIR:-$HOME/.local/bin}/managed-bash` |
| OpenCode plugin registration | `${XDG_CONFIG_HOME:-$HOME/.config}/opencode/plugins/managed-bash.js` |

Add `$HOME/.local/bin` to `PATH` when it is not already present. Restart OpenCode after an install or update so it loads the plugin and binary from the same release.

Check the installed binary:

```sh
printf '%s' '{"schema_version":1,"action":"version"}' | managed-bash version
```

Running `install.sh` again with the same archive is a no-op. An update stages an immutable release and switches the `current` symlink after validation. A failed update restores the previous pointer. The installer keeps previous complete releases for rollback and does not remove them automatically.

Uninstall from an extracted archive for the same host:

```sh
./uninstall.sh
```

Uninstall removes installer-owned release and registration paths. It does not inspect or delete `.managed_bash` directories in workspaces, and detached jobs that already started can finish.

## Selection and compatibility

The plugin resolves the CLI in this order:

1. `MANAGED_BASH_BINARY`, when set.
2. `managed-bash` from `PATH`.

The plugin resolves that selection to one physical release path for the lifetime of the OpenCode process. This prevents a loaded plugin from switching to a different binary when an installer update moves `current`. The plugin requires an exact product, release, and protocol match during its version handshake. It returns a structured tool error on mismatch or transport failure. It never calls built-in Bash as a fallback.

OpenCode can hide its built-in Bash tool while the plugin retains command permission checks. Disable the tool with `tools.bash: false`, then allow the command patterns that `managed_bash` may run under `permission.bash`.

## Security model

- OpenCode supplies the session ID, physical workspace path, and working directory. Model arguments cannot override them.
- Stateful requests must match the host-owned context. Cross-workspace reads look like missing jobs, and only the owner session can mutate a job.
- The runner removes trusted host variables before it starts the requested shell command.
- The installer rejects symlink-substituted path components, shared writable destinations, regular files at registration paths, and foreign symlinks.
- Install, update, and uninstall share one per-user lock. Release publication and the `current` switch use no-replace or atomic rename operations.
- Hard timeout, explicit cancellation, and output-limit enforcement terminate the whole process group, including descendants that ignore `SIGTERM`.

## Defaults and limitations

| Setting | Default |
|---------|---------|
| Wait timeout | 5 minutes |
| Idle checkpoint | 2 minutes |
| Hard timeout | 2 hours |
| Captured output limit | 100 MiB |

Protocol v1 has no restart reattachment. Preserved state does not make a running job reattachable after the runner or host restarts. The package command builds local archives only; GitHub Releases and CI publishing are outside this repository target. The installer supports per-user Linux and macOS installs, not privileged system-wide installs.

## Troubleshooting

- Run `make doctor` when a source build reports a Go or Bun version error.
- Run `command -v managed-bash` and the version request above when OpenCode reports `runner_transport_error` or `runner_protocol_error`.
- Run `opencode debug config` and confirm it lists `file://.../opencode/plugins/managed-bash.js` when the tool is absent.
- Restart OpenCode after an update. A process that loaded an older plugin must not pair it with a newer binary.
- Move or rename a file or foreign symlink that already occupies either registration path. The installer will not overwrite paths it does not own.
- Use physical HOME, XDG, workspace, and cwd paths. Symlink traversal fails closed.

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
| `SOURCE_DATE_EPOCH=<unix-seconds> make release-package` | Build and verify reproducible Linux/Darwin amd64/arm64 release archives under `dist/`. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make release-package-test` | Prove strict archive rejection, four-target byte reproducibility, normalized metadata, transactional install/uninstall, and native extracted and installed CLI acceptance. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make installed-opencode-e2e` | Install the native archive into temporary HOME/XDG paths and drive OpenCode 1.18.x through checkpoint, parallel control, cancellation, hard-timeout, cursor, permission-denial, completion, and no-fallback scenarios. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make e2e-test` | Run release package tests and the installed OpenCode E2E. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make verify` | Run every repository schema, runner, CLI, plugin, package, installer, and installed E2E gate in sequence. |
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

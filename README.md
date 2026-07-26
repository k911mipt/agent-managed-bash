# agent-managed-bash

Observable, cancellable long-running shell jobs for coding agents.

> Status: the protocol, detached runner, CLI, OpenCode plugin, reproducible archives, and per-user installer are implemented.

The v1 CLI contract permits a bounded one-shot read of the full captured prefix, up to 100 MiB. Capture appends retain only the accepted incoming prefix so repeated writes do not copy the full history.

The OpenCode plugin exposes one `managed_bash` tool. It does not fall back to OpenCode's built-in Bash tool when the binary is missing, incompatible, or returns malformed protocol output.

The repository also includes a standalone project skill at `.opencode/skills/managed-bash/`. It uses the native tool when the host exposes it, otherwise selects a separately installed CLI and finally a bundled, fileless tmux fallback. Installing the skill installs nothing else; fallback operation requires `tmux` to already be available.

## Prerequisites

- Go `1.26.5`
- Bun `1.3.14`
- `make`
- OpenCode `1.18.x` for explicit local plugin loading and installed E2E

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

The command writes four archives under `dist/`. Before a private GitHub Release exists, build them from a checkout or transfer the matching archive to the target machine.

Repository readers can download a private release with an authenticated GitHub CLI:

```sh
gh auth status
gh release download v0.1.0 \
  --repo k911mipt/agent-managed-bash \
  --pattern 'agent-managed-bash-*.tar.gz' \
  --pattern SHA256SUMS
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c SHA256SUMS
else
  shasum -a 256 -c SHA256SUMS
fi
```

Select and install only the archive matching the target host. Private release downloads require repository access and GitHub authentication. Public token-free downloads are deferred to issue #11.

### Manual archive installation

Choose the archive from `uname -s` and `uname -m`:

| Host | `uname -s` | `uname -m` | Archive suffix |
|------|------------|------------|----------------|
| Apple Silicon Mac | `Darwin` | `arm64` | `darwin-arm64` |
| Intel Mac | `Darwin` | `x86_64` | `darwin-amd64` |
| Linux amd64 | `Linux` | `x86_64` | `linux-amd64` |
| Linux arm64 | `Linux` | `aarch64` or `arm64` | `linux-arm64` |

For example, on an Apple Silicon Mac:

```sh
archive=agent-managed-bash-0.1.0-darwin-arm64.tar.gz
package=${archive##*/}
directory=${package%.tar.gz}
tar -xzf "$archive"
cd "$directory"
./install.sh
```

If the archive is still under `dist/`, set `archive=dist/agent-managed-bash-0.1.0-darwin-arm64.tar.gz`; the remaining commands stay the same.

The installer uses these per-user paths:

| Artifact | Default path |
|----------|--------------|
| Releases and `current` pointer | `${XDG_DATA_HOME:-$HOME/.local/share}/agent-managed-bash/` |
| CLI registration | `${MANAGED_BASH_BIN_DIR:-$HOME/.local/bin}/managed-bash` |
| OpenCode plugin bundle | `${XDG_DATA_HOME:-$HOME/.local/share}/agent-managed-bash/current/lib/opencode/managed-bash.js` |

Add `$HOME/.local/bin` to `PATH` when it is not already present. On macOS, put this in `~/.zshrc` and start a new shell:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

The installer does not edit OpenCode configuration. Generate a URL for the installed plugin so spaces and URL metacharacters in the data path are encoded correctly:

```sh
plugin_path="${XDG_DATA_HOME:-$HOME/.local/share}/agent-managed-bash/current/lib/opencode/managed-bash.js"
PLUGIN_PATH="$plugin_path" bun -e 'import { pathToFileURL } from "node:url"; console.log(pathToFileURL(process.env.PLUGIN_PATH).href)'
```

Add the printed URL to the `plugin` array in `${XDG_CONFIG_HOME:-$HOME/.config}/opencode/opencode.jsonc`:

```jsonc
{
  "plugin": [
    "file:///Users/alice/.local/share/agent-managed-bash/current/lib/opencode/managed-bash.js"
  ]
}
```

Restart OpenCode after an install or update so it loads the plugin and binary from the same release.

A future published package can be selected with the npm plugin spec `"@k911mipt/opencode-agent-managed-bash"`. Configure either the local file URL for development or the npm spec for a published release, not both at once.

Check the installed binary:

```sh
printf '%s' '{"schema_version":1,"action":"version"}' | managed-bash version
```

Running `install.sh` again with the same archive is a no-op unless it needs to remove an installer-owned legacy auto-discovery symlink. An update stages an immutable release and switches the `current` symlink after validation. A failed update restores the previous pointer. The installer keeps previous complete releases for rollback and does not remove them automatically.

Uninstall from an extracted archive for the same host:

```sh
./uninstall.sh
```

Uninstall removes installer-owned releases, the CLI registration, and any legacy installer-owned auto-discovery symlink. It does not edit OpenCode configuration, so remove the explicit plugin entry separately. It does not inspect or delete `.managed_bash` directories in workspaces, and detached jobs that already started can finish.

## Selection and compatibility

The plugin resolves the CLI in this order:

1. `MANAGED_BASH_BINARY`, when set.
2. `managed-bash` from `PATH`.

The plugin resolves that selection to one physical release path for the lifetime of the OpenCode process. This prevents a loaded plugin from switching to a different binary when an installer update moves `current`. The plugin requires an exact product, release, and protocol match during its version handshake. It returns a structured tool error on mismatch or transport failure. It never calls built-in Bash as a fallback.

The standalone skill has a separate selection boundary:

1. Its runtime instructions use the native `managed_bash` tool when available.
2. Its dispatcher uses `MANAGED_BASH_BINARY`, then `managed-bash` from `PATH`.
3. If neither CLI path exists, its bundled scripts use an existing `tmux` installation.

The tmux fallback stores metadata only in marked tmux sessions and never writes `.managed_bash`. It retains completed panes for status and a normalized tail of at most 200 rendered terminal lines until `remove`. Because `capture-pane` is a rendered terminal snapshot rather than an append-only byte log, cursor/range arguments and output-limit enforcement are explicitly unsupported. Cancellation and hard timeout perform best-effort pane-process cleanup rather than the Go runner's whole-process-group guarantee.

Tmux reads are filtered to jobs created from the same physical `pwd -P`; mutations also compare the caller-provided `MANAGED_BASH_SESSION_ID`. These checks prevent accidental crossover on a shared tmux socket, but they are not a security boundary against another process running as the same operating-system user.

OpenCode can hide its built-in Bash tool while the plugin retains command permission checks. Disable the tool with `tools.bash: false`, then allow the command patterns that `managed_bash` may run under `permission.bash`.

## Go runner security model

- OpenCode supplies the session ID, physical workspace path, and working directory. Model arguments cannot override them.
- Stateful requests must match the host-owned context. Cross-workspace reads look like missing jobs, and only the owner session can mutate a job.
- The runner removes trusted host variables before it starts the requested shell command.
- The installer rejects symlink-substituted path components, shared writable destinations, foreign CLI registrations, and foreign paths at the legacy auto-discovery location.
- Install, update, and uninstall share one per-user lock. Release publication and the `current` switch use no-replace or atomic rename operations.
- Hard timeout, explicit cancellation, and output-limit enforcement terminate the whole process group, including descendants that ignore `SIGTERM`.

## Defaults and limitations

| Setting | Default |
|---------|---------|
| Wait timeout | 5 minutes |
| Idle checkpoint | 2 minutes |
| Hard timeout | 2 hours |
| Captured output limit | 100 MiB |

`hard_timeout_ms` and `output_limit_bytes` configure `run`. `timeout_ms` and `idle_timeout_ms` configure the observational `wait`; neither wait timeout terminates the job.

Protocol v1 has no restart reattachment. Preserved state does not make a running job reattachable after the runner or host restarts. The package command itself builds local archives only; GitHub workflows orchestrate verification and private release publication around those outputs. The installer supports per-user Linux and macOS installs, not privileged system-wide installs.

## Continuous integration and private releases

Pull requests targeting `master` and pushes to `master` run the complete repository verification gate on Linux amd64 with a read-only token. Fork pull requests receive no release credentials. Obsolete runs for the same pull request or ref are cancelled to limit Actions usage.

A newly created trusted `vMAJOR.MINOR.PATCH` tag starts release verification only when the version matches `VERSION`, the tagged commit is on `master`, and GitHub associates it with a merged pull request to this repository. The tagged commit then runs the full native gate on Linux amd64/arm64 and macOS Intel/ARM64. Each native job supplies its matching verified archive as a three-day workflow artifact; a final job combines the four archives, creates `SHA256SUMS`, rechecks the tag, and publishes those exact files as a private GitHub Release without rebuilding them. Moved and deleted tag events do not enter the release DAG; repository-level immutable-release and tag policies remain part of the public-release hardening in issue #11.

Reproduce release bytes locally from the tagged commit with:

```sh
SOURCE_DATE_EPOCH=$(git show -s --format=%ct HEAD) make release-package
```

Validate the checked-in workflow contract with `make workflow-test`. On GitHub Free, a private repository owned by an account without a valid payment method stops running jobs when the included Actions quota is exhausted, so no paid overage or separate budget mutation is possible. The private setup conserves that quota by keeping routine pull-request and `master` verification on Linux amd64 and running the full four-platform matrix only for release tags. After the repository becomes public, issue #11 expands regular CI to the full supported matrix and adds required checks plus public token-free distribution.

## Troubleshooting

- Run `make doctor` when a source build reports a Go or Bun version error.
- Run `command -v managed-bash` and the version request above when OpenCode reports `runner_transport_error` or `runner_protocol_error`.
- Run `opencode debug config` and confirm it lists `file://.../agent-managed-bash/current/lib/opencode/managed-bash.js` when the tool is absent.
- Restart OpenCode after an update. A process that loaded an older plugin must not pair it with a newer binary.
- Move or rename a file or foreign symlink that occupies the CLI registration or legacy auto-discovery path. The installer will not overwrite paths it does not own.
- Use physical HOME, XDG, workspace, and cwd paths. Symlink traversal fails closed.

## Root commands

| Command | Purpose |
|---------|---------|
| `make doctor` | Check that the installed Go and Bun versions match the repository pins. |
| `make workflow-test` | Validate workflow triggers, permissions, runner matrix, immutable action pins, artifact handoff, and release publication structure. |
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
| `make portable-skill-test` | Exercise CLI-before-tmux selection, tmux lifecycle and diagnostics, reduced-semantics errors, and real CLI/tmux conformance fixtures. |
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

Action subcommands fail immediately with usage guidance when stdin is a terminal; pipe or redirect the JSON request instead of invoking `managed-bash list` or another action interactively. Validation responses may include bounded `field`, `reason`, `expected`, and `actual` details, which the OpenCode plugin renders after the stable error code and message.

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

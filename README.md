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

### Public pinned install

Use one explicit `vMAJOR.MINOR.PATCH` tag. The command below downloads the
bootstrap from that immutable GitHub Release and lets it select the archive
for the current host. It needs no GitHub token.

```sh
tag=v0.1.0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' 0 HUP INT TERM
curl --fail --location --proto '=https' --proto-redir '=https' \
  "https://github.com/k911mipt/agent-managed-bash/releases/download/$tag/install-release.sh" \
  >"$tmp/install-release.sh"
chmod 700 "$tmp/install-release.sh"
"$tmp/install-release.sh" "$tag"
```

The bootstrap accepts exactly one `vMAJOR.MINOR.PATCH` argument. It maps
Darwin arm64 and amd64, and Linux arm64 and amd64, downloads `SHA256SUMS` and
the matching archive over HTTPS, verifies one canonical checksum, validates
the archive layout, and only then invokes the existing per-user installer.
The loopback base URL override exists for local tests only:
`MANAGED_BASH_RELEASE_BASE_URL=http://127.0.0.1:<port>/<path>`.

The release contains four native archives, `install-release.sh`, the npm
plugin tarball, `SHA256SUMS`, and five SPDX 2.3 SBOM files. The archive and
package bytes are verified before publication and are not rebuilt during
publication or recovery.

To build the same native archives locally, use a checkout and an explicit
source timestamp:

```sh
SOURCE_DATE_EPOCH=1700000000 make release-package
```

The command writes four archives under `dist/`. This is a local development
path, not a substitute for the pinned public release command.

### Verify release assets

For an additional release-level check, download the public assets with the
GitHub CLI. `gh release verify-asset` is optional because the bootstrap already
checks `SHA256SUMS` before extraction.

```sh
tag=v0.1.0
version=${tag#v}
gh release download "$tag" \
  --repo k911mipt/agent-managed-bash \
  --pattern 'agent-managed-bash-*.tar.gz' \
  --pattern 'k911mipt-opencode-agent-managed-bash-*.tgz' \
  --pattern 'install-release.sh' \
  --pattern 'SHA256SUMS' \
  --pattern '*.spdx.json'
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c SHA256SUMS
else
  shasum -a 256 -c SHA256SUMS
fi

for asset in \
  "agent-managed-bash-${version}-linux-amd64.tar.gz" \
  "agent-managed-bash-${version}-linux-arm64.tar.gz" \
  "agent-managed-bash-${version}-darwin-amd64.tar.gz" \
  "agent-managed-bash-${version}-darwin-arm64.tar.gz" \
  "install-release.sh" \
  "k911mipt-opencode-agent-managed-bash-${version}.tgz"; do
  gh release verify-asset "$tag" "$asset" --repo k911mipt/agent-managed-bash
done

for asset in \
  agent-managed-bash-*.tar.gz \
  install-release.sh \
  k911mipt-opencode-agent-managed-bash-*.tgz \
  *.spdx.json \
  SHA256SUMS; do
  gh attestation verify "$asset" \
    --repo k911mipt/agent-managed-bash \
    --signer-workflow k911mipt/agent-managed-bash/.github/workflows/release.yml \
    --predicate-type https://slsa.dev/provenance/v1 \
    --source-ref "refs/tags/$tag"
done

for asset in \
  "agent-managed-bash-${version}-linux-amd64.tar.gz" \
  "agent-managed-bash-${version}-linux-arm64.tar.gz" \
  "agent-managed-bash-${version}-darwin-amd64.tar.gz" \
  "agent-managed-bash-${version}-darwin-arm64.tar.gz" \
  "k911mipt-opencode-agent-managed-bash-${version}.tgz"; do
  gh attestation verify "$asset" \
    --repo k911mipt/agent-managed-bash \
    --signer-workflow k911mipt/agent-managed-bash/.github/workflows/release.yml \
    --predicate-type https://spdx.dev/Document/v2.3 \
    --source-ref "refs/tags/$tag"
done
```

The first loop checks the release binding for the six primary downloadable
assets. The second checks SLSA provenance for all twelve release assets. The
third checks the five SPDX subject attestations against the four native
archives and the npm tarball. For each of those five results, compare the
verified predicate content and digest with the corresponding downloadable
`<asset>.spdx.json`. Task 14 automates that comparison. Verifying provenance
for an SBOM file alone does not establish its subject and predicate binding.
`gh attestation verify` requires a current GitHub CLI and public network
access.

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

For a published release, install the matching npm plugin in the OpenCode
project and register one npm spec:

```sh
npm install --ignore-scripts --no-audit --no-fund \
  @k911mipt/opencode-agent-managed-bash@0.1.0
```

```jsonc
{
  "plugin": [
    "@k911mipt/opencode-agent-managed-bash@0.1.0"
  ]
}
```

The npm package contains only the OpenCode plugin. It does not contain or
install the native `managed-bash` binary. Install the matching native archive
with the pinned bootstrap first. Use either the npm spec or an encoded local
`file://` URL for development. Never register both entries.

Check the installed binary:

```sh
printf '%s' '{"schema_version":1,"action":"version"}' | managed-bash version
```

Running `install.sh` again with the same archive is a no-op unless it needs to
remove an installer-owned legacy auto-discovery symlink. An update stages an
immutable release and switches the `current` symlink after validation. If a
reinstall fails, the installer preserves the current release, symlink target,
and target inode, and does not fall back to another binary. This is failed
reinstall preservation, not a public rollback command.

The installer has a transactional internal rollback guarantee while it is
switching releases. It keeps previous complete releases until uninstall.
Cross-version public rollback needs a second immutable release and is
deferred. The first release has no public rollback command.

Uninstall from an extracted archive for the same host:

```sh
./uninstall.sh
```

Uninstall removes installer-owned releases, the CLI registration, and any
legacy installer-owned auto-discovery symlink. It does not edit OpenCode
configuration. Remove the matching npm spec or local `file://` entry from the
`plugin` array yourself, then run `opencode debug config` and confirm the entry
is gone. Uninstall does not inspect or delete `.managed_bash` directories in
workspaces, and detached jobs that already started can finish.

## Selection and compatibility

The plugin resolves the CLI in this order:

1. `MANAGED_BASH_BINARY`, when set.
2. `managed-bash` from `PATH`.

The plugin resolves that selection to one physical release path for the
lifetime of the OpenCode process. This prevents a loaded plugin from switching
to a different binary when an installer update moves `current`. The plugin
requires the exact product `managed-bash`, the same release version as the
plugin, and protocol version `1` during its handshake. It returns a structured
tool error on a product, release, protocol, or transport mismatch. It never
calls built-in Bash as a fallback.

The supported host pairing is OpenCode `1.18.x` with the plugin API dependency
`@opencode-ai/plugin` `1.18.4`, the npm plugin version from `VERSION`, and the
native archive built from the same `VERSION`. Do not pair releases from
different versions.

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

Protocol v1 has no restart reattachment. Preserved state does not make a
running job reattachable after the runner or host restarts. The
`release-package` target builds local archives only. The GitHub workflow
verifies, checksums, attests, and publishes those exact bytes without
rebuilding or repacking them. The installer supports per-user Linux and macOS
installs, not privileged system-wide installs.

## Continuous integration and public releases

Pull requests targeting `master` and pushes to `master` run `Verify
<target>` on Linux amd64 and arm64, and macOS Intel and ARM64. The required
`Verification gate` fails unless all four matrix jobs succeed. The workflow
has read-only repository permissions and cancels obsolete runs for the same
pull request or ref.

A newly created `vMAJOR.MINOR.PATCH` tag starts release verification only when
the version matches `VERSION`, the tagged commit is on `master`, and GitHub
associates it with a merged pull request. Producer jobs verify the four native
archives, the bootstrap, and the npm tarball. Separate jobs validate five
SPDX 2.3 SBOMs, assemble `SHA256SUMS`, and bind the candidate to its receipts.
The publication jobs consume those exact candidate bytes. They do not build or
repack a release.

The first publish uses the protected `release` environment twice. The first
approval stages the npm package and GitHub draft with provenance and SBOM
attestations. The second approval finalizes the immutable GitHub Release. If
npm cannot configure the trusted publisher before the package exists, the
release operator supplies a temporary bootstrap version and token. The
workflow passes those credentials only to the child process running the exact
npm publish command. After staging, the release operator verifies npm SRI and
provenance, configures and reads back the GitHub Actions trusted publisher,
revokes and deletes the temporary token, secret, and version variable, then
approves finalization. OIDC is configured and read back during that transition.
Unless npm allowed preconfiguration, token-free OIDC publication is
operationally proven on the next version.

If publication needs recovery, the workflow accepts the original run ID and
the immutable tag. It checks the tagged commit, release workflow blob, and the
original candidate and control receipt before reusing the artifacts. Recovery
does not rebuild, repack, or continue from a draft without the original
receipts.

Reproduce release bytes locally from the tagged commit with:

```sh
SOURCE_DATE_EPOCH=$(git show -s --format=%ct HEAD) make release-package
```

Validate the checked-in workflow contract with `make workflow-test`.

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
| `make npm-package` | Build the versioned OpenCode plugin tarball from the bundled plugin and canonical `VERSION`. |
| `make npm-package-test` | Validate the npm file set, manifest, release identity, import surface, and failed-pack preservation. |
| `make opencode-plugin-config-test` | Load one explicit local plugin URL through OpenCode 1.18.x and validate the resolved config. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make release-package` | Build and verify reproducible Linux/Darwin amd64/arm64 release archives under `dist/`. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make release-package-test` | Prove strict archive rejection, four-target byte reproducibility, normalized metadata, transactional install/uninstall, and native extracted and installed CLI acceptance. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make public-install-test` | Exercise the bootstrap against a bounded loopback release fixture, including checksum, host, archive safety, interruption, and current-state preservation failures. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make installed-opencode-e2e` | Install the native archive into temporary HOME/XDG paths and drive OpenCode 1.18.x through checkpoint, parallel control, cancellation, hard-timeout, cursor, permission-denial, completion, and no-fallback scenarios. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make e2e-test` | Run release package tests and the installed OpenCode E2E. |
| `SOURCE_DATE_EPOCH=<unix-seconds> make verify` | Run the aggregate schema, runner, CLI, plugin, workflow, archive, installer, and installed E2E gate used by CI. |
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

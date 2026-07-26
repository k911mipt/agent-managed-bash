# Repository Instructions

## Scope

These instructions apply to the entire repository. Keep this file limited to stable repository rules. Ticket state, temporary decisions, and implementation plans belong outside the repository.

## Architecture

- `managed-bash` is one Go binary. It exposes the public JSON-over-stdin CLI and launches its detached bootstrap, runner, and guardian modes from the same executable.
- `plugins/opencode/` is a TypeScript+Bun OpenCode plugin. Keep it as a thin adapter for permissions, trusted OpenCode context, protocol transport, presentation, and session cleanup requests.
- For native-tool and installed-CLI jobs, the Go binary owns process execution, persisted state, output capture, hard timeouts, and process-group cleanup. The plugin may request cancellation, but must not perform those responsibilities itself.
- The standalone portable skill may use its bundled tmux fallback only when the native tool and CLI are absent. That reduced backend stores metadata in marked tmux sessions, never writes `.managed_bash`, returns explicit unsupported errors for byte cursor/range and output-limit semantics, and documents best-effort pane-process cleanup.
- JSON Schema draft 2020-12 files under `schemas/v1/` are the protocol and persisted-state source of truth.
- The product has no permanent daemon. Do not introduce one without an explicit design change.

## Supported Environment

- Go: `1.26.5`
- Bun: `1.3.14`
- Package installation: `bun install --frozen-lockfile`
- Supported runtime targets: Linux and macOS on amd64 and arm64

Run `make doctor` before diagnosing toolchain failures. Keep Go and Bun pins exact unless the change explicitly upgrades them.
Scripts intended for both supported operating systems must not depend on GNU-only command flags.

## Generated Protocol Code

Do not edit these files by hand:

- `internal/protocol/generated/models.gen.go`
- `plugins/opencode/src/generated/protocol.gen.ts`

Change `schemas/v1/` or the generator, run `make schema-generate`, and commit the generated outputs with the source change. Run `make schema-generated-check` to prove generation is reproducible.

Keep Go and TypeScript consumers on the same fixture manifest under `fixtures/v1/`. A protocol change is incomplete if either language accepts a different contract.

## Runtime Invariants

- Runtime state lives at `<workspace>/.managed_bash/jobs/`. Code outside the Go runner must not delete, relocate, or rewrite a workspace's `.managed_bash` tree.
- Wait and idle checkpoints return control without terminating a job. For Go-runner jobs, hard timeout, explicit cancellation, and output-limit enforcement terminate the process group. The tmux fallback does not support output-limit enforcement and performs best-effort pane-process termination.
- Preserve whole-process-group cleanup for Go-runner jobs, including descendants that ignore `SIGTERM`. Do not claim that guarantee for the reduced tmux fallback.
- For native-tool and plugin-managed CLI jobs, treat session ID, workspace path, and cwd from the host as trusted context. Model-supplied values must not override them.
- Keep Go-runner workspace paths physical, canonical, and symlink-safe. Cross-workspace reads look like missing jobs; mutations require the owner session.
- The tmux fallback derives workspace identity from physical `pwd -P` and uses caller-supplied session identity only to prevent accidental crossover. Do not describe that same-user tmux metadata as a security boundary.
- The standalone skill's CLI adapter also receives caller-supplied session/workspace identity. Do not describe it as equivalent to the OpenCode plugin's trusted-context boundary.
- The plugin must fail with a structured tool error when the binary is missing, malformed, or incompatible. It must never fall back to built-in Bash.
- `remove` deletes one terminal job. It is not package uninstall and must reject active jobs.
- Protocol v1 does not support restart reattachment. Do not imply that preserved state makes active jobs reattachable after a restart.

## Change Workflow

1. Read the affected implementation and its focused tests before editing.
2. Add or update the narrowest behavioral test before production code when behavior changes.
3. Use the repository Make targets instead of bypassing their flags or toolchain pins.
4. Run focused checks for the changed area, then the relevant aggregate checks.
5. Exercise CLI or OpenCode changes through the real binary or plugin surface, not only imported helpers.

Keep tests deterministic. Use temporary workspaces and bounded waits instead of fixed sleeps. Preserve race-enabled Go coverage for concurrent runner and CLI behavior.

## Verification Commands

The rows are cumulative: run every row affected by a change. Any Go implementation change also requires `make go-check`.

| Area | Minimum commands |
|---|---|
| Toolchain | `make doctor` |
| Protocol and generated state | `make schema-check` |
| Runner | `make runner-test` |
| CLI | `make cli-race-test` and `make cli-acceptance` |
| OpenCode plugin | `make plugin-test` and `make plugin-typecheck` |
| Repository-wide Go changes | `make go-check` |

Run `sh tests/doctor_test.sh` or `sh tests/schema_generation_test.sh` when changing their corresponding scripts or failure contracts.

## Worktree Hygiene

- Do not commit `.managed_bash/`, `.omo/`, dependency directories, binaries, archives, coverage data, or other ignored build outputs.
- Do not delete or rewrite unrelated local files. Other people and agents may share the worktree.
- Keep commits atomic and use the repository's plain imperative English commit style.
- Do not add co-author, generated-by, AI, model, agent, or tool attribution unless the user explicitly requests it.
- Do not force-push or bypass required review and checks.

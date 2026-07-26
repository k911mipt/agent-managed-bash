---
name: managed-bash
description: Run and observe long-lived shell commands through the first available managed-bash backend: native tool, installed CLI, or a self-contained tmux fallback. Use for cancellable background commands, bounded waits, status checks, retained output, and job cleanup in coding-agent environments.
compatibility: OpenCode and coding agents that support project skills; the tmux fallback requires POSIX sh and an existing tmux installation.
---

# Managed Bash

Use one action vocabulary for long-running commands without composing raw tmux commands.

## Backend selection

1. If the host exposes the native `managed_bash` tool, call it directly. Do not invoke the bundled dispatcher.
2. Otherwise call [`scripts/managed-bash`](./scripts/managed-bash). It selects `MANAGED_BASH_BINARY`, then `managed-bash` on `PATH`, then `tmux`.
3. Read stderr diagnostics to see the selected backend. A present backend that fails is returned directly; selection does not cascade after execution starts.

The skill installs no binary, package, config, or symlink. The fallback assumes `tmux` is already installed.

## Actions

```sh
scripts/managed-bash run [--hard-timeout-ms N] -- '<shell command>'
scripts/managed-bash wait JOB_ID [--timeout-ms N] [--idle-timeout-ms N]
scripts/managed-bash status JOB_ID
scripts/managed-bash output JOB_ID
scripts/managed-bash cancel JOB_ID
scripts/managed-bash remove JOB_ID
scripts/managed-bash list
scripts/managed-bash version
```

Use `MANAGED_BASH_SESSION_ID` for a stable caller identity. The CLI adapter accepts `MANAGED_BASH_WORKSPACE_PATH`; the tmux fallback deliberately derives workspace identity from its physical `pwd -P`. Tests and isolated callers may set `MANAGED_BASH_TMUX_SOCKET` to a safe tmux socket name.

## Tmux fallback contract

- One marked, retained tmux session represents one job.
- Job metadata lives only in tmux user options. The fallback never creates `.managed_bash`.
- `capture-pane` provides a normalized tail of at most 200 rendered terminal lines, not an append-only byte log.
- Raw cursor/range arguments and `--output-limit-bytes` return an actionable unsupported error.
- Job state disappears when the tmux server exits. A vanished session is `job_not_found`, not durable `runner_lost`.
- Cancellation and hard timeout terminate the pane process on a best-effort basis; they do not provide the Go runner's process-group guarantee.

See [`references/backend-contract.md`](./references/backend-contract.md) for exact defaults, statuses, and differences. Interactive installers remain a direct tmux workflow outside this skill.

## Verification

From the repository root:

```sh
make portable-skill-test
python3 /path/to/opencode-skill-author/scripts/validate_skill.py .opencode/skills/managed-bash
```

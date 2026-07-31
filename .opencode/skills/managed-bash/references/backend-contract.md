# Backend contract

## Selection

The model chooses the native `managed_bash` tool when the host exposes it. The bundled dispatcher then selects only among shell-visible backends:

1. executable named by `MANAGED_BASH_BINARY`;
2. `managed-bash` on `PATH`;
3. `tmux` on `PATH` or named by `MANAGED_BASH_TMUX_BINARY`.

Selection is based on availability only. Protocol, command, or runtime failure after selection does not trigger another backend.

## Common actions

All backends expose `start`, `run`, `wait`, `status`, `output`, `cancel`, `remove`, `list`, and `version`.

- `start` launches a detached job and returns its metadata immediately.
- `run` launches a job and observes from the beginning until terminal completion, output idle, or the observation timeout.
- Successful `run` and `wait` responses report `terminal`, `output_idle`, or `observation_timeout` together with the observation and output.

Common defaults:

| Setting | Default |
|---------|---------|
| Wait timeout | 300000 ms |
| Idle checkpoint | 120000 ms |
| Hard timeout | 7200000 ms |
| Reported output-limit compatibility value | 104857600 bytes, not enforced by tmux |

Common statuses where supported: `running`, `succeeded`, `nonzero_exit`, `signal_exit`, `cancelled`, and `hard_timeout`.

## Tmux-specific behavior

- Sessions use the `managed-bash-` prefix plus `@managed_bash_backend=tmux`.
- After the command exits, the bundled runner records its exit code in a session option and keeps the pane alive until `remove`; this avoids tmux death-banner output on versions that cannot disable it.
- Session options retain command, owner, workspace, cwd, timestamps, hard timeout, exit code, and derived termination reason while the tmux server lives.
- `wait` polls status and rendered output with one-second checkpoint precision.
- `run` uses the same polling and checkpoint behavior after it launches the tmux job.
- `output` returns a bounded tail of at most 200 rendered terminal lines with a synthetic zero-based range. Trailing blank lines are normalized away, and callers must not use the reported range as a stable incremental cursor.
- `cancel` and hard timeout signal the pane process. Descendant cleanup is best effort.
- Natural exit codes from 129 through 192 are interpreted using the conventional `128 + signal` shell encoding; 128 and 193 through 255 remain `nonzero_exit`. An explicit `exit 143` is therefore indistinguishable from SIGTERM in this reduced backend.
- Reads are limited to the same physical `pwd -P`; mutations also compare the caller-provided session ID. This prevents accidental crossover, not malicious access by another process under the same OS user.
- The standalone CLI adapter's session/workspace inputs are likewise caller-supplied and advisory; only the OpenCode plugin provides host-trusted context.
- `output_limit`, raw cursor continuation, byte ranges, `runner_lost`, cross-restart attachment, and `.managed_bash` persistence are unsupported.

The tmux fallback reports unsupported inputs instead of silently approximating protocol guarantees.

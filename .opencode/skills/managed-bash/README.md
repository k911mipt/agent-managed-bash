[Русская версия](./README.ru.md)

# Managed Bash Skill

Install this skill when a coding agent needs observable, cancellable long-running shell jobs but may not provide OpenCode's native `managed_bash` tool. It removes hand-written tmux command sequences and keeps backend selection deterministic and visible.

## When It Pays Off

- Use it for builds, tests, servers, and other commands that outlive one tool call.
- It saves time when the same workflow must work with a native tool, a separately installed CLI, or only tmux.
- Skip it for interactive installers and short foreground commands.

## What It Includes

- [`SKILL.md`](./SKILL.md): runtime backend-selection and action playbook.
- [`scripts/managed-bash`](./scripts/managed-bash): public dispatcher.
- [`scripts/cli-backend`](./scripts/cli-backend): installed-CLI adapter.
- [`scripts/tmux-backend`](./scripts/tmux-backend): fileless tmux fallback.
- [`scripts/tmux-lib.sh`](./scripts/tmux-lib.sh): tmux observation, access filtering, and response helpers.
- [`scripts/tmux-runner`](./scripts/tmux-runner): records natural command exit status in tmux memory before the pane exits.
- [`references/backend-contract.md`](./references/backend-contract.md): defaults, statuses, and reduced tmux semantics.

The skill installs nothing else. For fallback operation, `tmux` must already be on `PATH`.

## First Use

Ask: `Run my long test in the background, wait until it becomes idle, and show me the latest output.`

## Runtime Docs

See [`SKILL.md`](./SKILL.md) for exact commands and [`references/backend-contract.md`](./references/backend-contract.md) for backend differences.

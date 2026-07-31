[English version](./README.md)

# Skill Managed Bash

Установите этот skill, когда coding agent должен управлять наблюдаемыми и отменяемыми долгими shell-командами, но может не иметь нативного OpenCode tool `managed_bash`. Skill убирает ручную сборку tmux-команд и делает выбор backend детерминированным и видимым.

## Когда окупается

- Используйте для сборок, тестов, серверов и других команд, живущих дольше одного tool call.
- Экономит время, когда один workflow должен работать через native tool, отдельно установленный CLI или только tmux.
- Не используйте для интерактивных установщиков и коротких foreground-команд.

## Что внутри

- [`SKILL.md`](./SKILL.md): runtime playbook выбора backend и действий.
- [`scripts/managed-bash`](./scripts/managed-bash): публичный dispatcher.
- [`scripts/cli-backend`](./scripts/cli-backend): адаптер отдельно установленного CLI.
- [`scripts/tmux-backend`](./scripts/tmux-backend): tmux fallback без файлового state.
- [`scripts/tmux-lib.sh`](./scripts/tmux-lib.sh): helpers для tmux observation, access filtering и responses.
- [`scripts/tmux-runner`](./scripts/tmux-runner): сохраняет natural exit status в памяти tmux до завершения pane.
- [`references/backend-contract.md`](./references/backend-contract.md): defaults, statuses и сокращённая tmux semantics.

Skill больше ничего не устанавливает. Для fallback-режима `tmux` должен уже находиться в `PATH`.

## Первый запуск

Попросите: `Запусти мой долгий тест и покажи вывод; верни управление раньше, если вывод станет idle.` Отдельный фоновый запуск нужен только для последующего управления или наблюдения за job.

## Runtime-документация

Точные команды находятся в [`SKILL.md`](./SKILL.md), различия backend описаны в [`references/backend-contract.md`](./references/backend-contract.md).

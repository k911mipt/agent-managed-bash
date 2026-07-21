#!/bin/sh

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
required_go=

while read -r directive version _; do
    if [ "$directive" = "go" ]; then
        required_go=$version
        break
    fi
done <"$ROOT/go.mod"

IFS= read -r required_bun <"$ROOT/.bun-version"

if [ -z "$required_go" ] || [ -z "$required_bun" ]; then
    printf '%s\n' "doctor: repository toolchain pins are incomplete" >&2
    exit 2
fi

if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' "doctor: Go $required_go is required, but 'go' was not found; install Go $required_go and ensure it is on PATH" >&2
    exit 1
fi

if ! go_output=$(GOTOOLCHAIN=local go version 2>&1); then
    printf '%s\n' "doctor: Go $required_go is required, but 'go version' failed: $go_output; reinstall Go $required_go" >&2
    exit 1
fi

set -- $go_output
found_go=${3:-}
found_go=${found_go#go}

if [ "$found_go" != "$required_go" ]; then
    printf '%s\n' "doctor: Go $required_go is required, but found $found_go; install Go $required_go and ensure it is first on PATH" >&2
    exit 1
fi

if ! command -v bun >/dev/null 2>&1; then
    printf '%s\n' "doctor: Bun $required_bun is required, but 'bun' was not found; install Bun $required_bun and ensure it is on PATH" >&2
    exit 1
fi

if ! found_bun=$(bun --version 2>&1); then
    printf '%s\n' "doctor: Bun $required_bun is required, but 'bun --version' failed: $found_bun; reinstall Bun $required_bun" >&2
    exit 1
fi

if [ "$found_bun" != "$required_bun" ]; then
    printf '%s\n' "doctor: Bun $required_bun is required, but found $found_bun; install Bun $required_bun and ensure it is first on PATH" >&2
    exit 1
fi

printf '%s\n' "doctor: toolchains ok (Go $required_go, Bun $required_bun)"

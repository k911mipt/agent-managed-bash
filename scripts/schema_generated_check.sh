#!/bin/sh

set -eu

case $0 in
    */*) script_dir=${0%/*} ;;
    *) script_dir=. ;;
esac
ROOT=$(CDPATH= cd -- "$script_dir/.." && pwd)
MAKE_BIN=${MAKE:-make}
case $MAKE_BIN in
    */*) ;;
    *) MAKE_BIN=$(command -v "$MAKE_BIN") ;;
esac
GENERATED=$(mktemp -d)

trap 'rm -rf "$GENERATED"' EXIT HUP INT TERM

generate_into() {
    output_root=$1
    schema_root=$2
    "$MAKE_BIN" --no-print-directory -s -C "$ROOT" schema-generate \
        GO_GENERATED="$output_root/internal/protocol/generated/models.gen.go" \
        TS_GENERATED="$output_root/plugins/opencode/src/generated/protocol.gen.ts" \
        SCHEMA_ROOT="$schema_root"
}

compare() {
    expected=$1
    actual=$2
    label=$3

    if ! cmp -s "$expected" "$actual"; then
        printf '%s\n' "schema-generated-check: $label differs" >&2
        diff -u "$expected" "$actual" >&2 || true
        exit 1
    fi
}

mkdir -p "$GENERATED/first-source/schemas" "$GENERATED/second-source/schemas"
cp -R "$ROOT/schemas/v1" "$GENERATED/first-source/schemas/v1"
cp -R "$ROOT/schemas/v1" "$GENERATED/second-source/schemas/v1"

generate_into "$GENERATED/first" "$GENERATED/first-source/schemas/v1"
generate_into "$GENERATED/second" "$GENERATED/second-source/schemas/v1"

compare \
    "$GENERATED/first/internal/protocol/generated/models.gen.go" \
    "$GENERATED/second/internal/protocol/generated/models.gen.go" \
    "Go output between independent runs"
compare \
    "$GENERATED/first/plugins/opencode/src/generated/protocol.gen.ts" \
    "$GENERATED/second/plugins/opencode/src/generated/protocol.gen.ts" \
    "TypeScript output between independent runs"
compare \
    "$ROOT/internal/protocol/generated/models.gen.go" \
    "$GENERATED/first/internal/protocol/generated/models.gen.go" \
    "checked-in Go output"
compare \
    "$ROOT/plugins/opencode/src/generated/protocol.gen.ts" \
    "$GENERATED/first/plugins/opencode/src/generated/protocol.gen.ts" \
    "checked-in TypeScript output"

printf '%s\n' "schema-generated-check: generated files are reproducible"

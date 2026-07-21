#!/bin/sh

set -eu

case $0 in
    */*) script_dir=${0%/*} ;;
    *) script_dir=. ;;
esac
ROOT=$(CDPATH= cd -- "$script_dir/.." && pwd)
MAKE_BIN=${MAKE:-make}
TMP=$(mktemp -d)

trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fake_go=$TMP/fail-go
go_output=$TMP/models.gen.go
ts_output=$TMP/protocol.gen.ts
printf '%s\n' '#!/bin/sh' 'exit 23' >"$fake_go"
chmod +x "$fake_go"
printf '%s\n' 'go sentinel' >"$go_output"
printf '%s\n' 'ts sentinel' >"$ts_output"

if "$MAKE_BIN" --no-print-directory -s -C "$ROOT" schema-generate \
    GO_GENERATOR="$fake_go" \
    GO_GENERATED="$go_output" \
    TS_GENERATED="$ts_output"; then
    printf '%s\n' 'schema generation unexpectedly succeeded with failing Go generator' >&2
    exit 1
fi

test "$(cat "$go_output")" = 'go sentinel'
test "$(cat "$ts_output")" = 'ts sentinel'

"$MAKE_BIN" --no-print-directory -s -C "$ROOT" schema-generated-check

printf '%s\n' "schema generation contract passed"

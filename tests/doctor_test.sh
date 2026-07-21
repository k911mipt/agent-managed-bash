#!/bin/sh

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
MAKE_BIN=${MAKE:-$(command -v make)}
FIXTURES=$(mktemp -d)
failures=0

trap 'rm -rf "$FIXTURES"' EXIT HUP INT TERM

create_fake_tool() {
    tool_dir=$1
    tool_name=$2
    version_output=$3

    mkdir -p "$tool_dir"
    cat >"$tool_dir/$tool_name" <<EOF
#!/bin/sh
printf '%s\n' '$version_output'
EOF
    chmod +x "$tool_dir/$tool_name"
}

expect_doctor_failure() {
    case_name=$1
    tool_dir=$2
    shift 2
    case_failures=$failures

    set +e
    output=$(PATH="$tool_dir:/usr/bin:/bin" "$MAKE_BIN" --no-print-directory -s -C "$ROOT" doctor 2>&1)
    status=$?
    set -e

    if [ "$status" -eq 0 ]; then
        printf '%s\n' "FAIL: $case_name: make doctor unexpectedly succeeded" >&2
        failures=$((failures + 1))
    fi

    for expected in "$@"; do
        case $output in
            *"$expected"*) ;;
            *)
                printf '%s\n' "FAIL: $case_name: output did not contain '$expected'" >&2
                failures=$((failures + 1))
                ;;
        esac
    done

    if [ "$failures" -ne "$case_failures" ]; then
        printf '  make doctor output: %s\n' "$output" >&2
    fi
}

create_fake_tool "$FIXTURES/missing-go" bun "1.3.14"
expect_doctor_failure "missing Go" "$FIXTURES/missing-go" "Go" "1.26.5" "install"

create_fake_tool "$FIXTURES/missing-bun" go "go version go1.26.5 linux/amd64"
expect_doctor_failure "missing Bun" "$FIXTURES/missing-bun" "Bun" "1.3.14" "install"

create_fake_tool "$FIXTURES/unsupported-go" go "go version go1.25.0 linux/amd64"
create_fake_tool "$FIXTURES/unsupported-go" bun "1.3.14"
expect_doctor_failure "unsupported Go" "$FIXTURES/unsupported-go" "Go" "1.26.5" "1.25.0"

create_fake_tool "$FIXTURES/unsupported-bun" go "go version go1.26.5 linux/amd64"
create_fake_tool "$FIXTURES/unsupported-bun" bun "1.2.0"
expect_doctor_failure "unsupported Bun" "$FIXTURES/unsupported-bun" "Bun" "1.3.14" "1.2.0"

if [ "$failures" -ne 0 ]; then
    printf '%s\n' "doctor contract failed with $failures assertion(s)" >&2
    exit 1
fi

printf '%s\n' "doctor contract passed"

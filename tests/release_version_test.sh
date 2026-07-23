#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM

release_version=$(tr -d '\r\n' <"$root/VERSION")
binary="$stage/managed-bash"

make --no-print-directory -C "$root" cli-build CLI_BINARY="$binary"
response=$(printf '%s' '{"schema_version":1,"action":"version"}' | "$binary" version)
override_binary="$stage/managed-bash-override"
make --no-print-directory -C "$root" cli-build CLI_BINARY="$override_binary" RELEASE_VERSION=9.9.9
override_response=$(printf '%s' '{"schema_version":1,"action":"version"}' | "$override_binary" version)

case "$response" in
  *"\"binary_version\":\"$release_version\""*) ;;
  *)
    printf '%s\n' "release version mismatch: expected $release_version, got $response" >&2
    exit 1
    ;;
esac

test "$override_response" = "$response"

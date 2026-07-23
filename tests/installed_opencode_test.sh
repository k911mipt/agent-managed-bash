#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
: "${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required}"
stage=$(mktemp -d)
stage=$(CDPATH= cd -- "$stage" && pwd -P)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
version=$(tr -d '\r\n' <"$root/VERSION")

case $(uname -s) in
	Linux) native_os=linux ;;
	Darwin) native_os=darwin ;;
	*) printf '%s\n' "unsupported native operating system: $(uname -s)" >&2; exit 1 ;;
esac
case $(uname -m) in
	x86_64|amd64) native_arch=amd64 ;;
	aarch64|arm64) native_arch=arm64 ;;
	*) printf '%s\n' "unsupported native architecture: $(uname -m)" >&2; exit 1 ;;
esac

native_name="agent-managed-bash-$version-$native_os-$native_arch"
tar -xzf "$root/dist/$native_name.tar.gz" -C "$stage"
opencode=$(command -v opencode)
case $("$opencode" --version) in
	1.18.*) ;;
	*) printf '%s\n' 'OpenCode 1.18.x is required' >&2; exit 1 ;;
esac
bun run "$root/e2e/installed_opencode_test.ts" "$stage/$native_name" "$opencode"
bun run "$root/e2e/installed_opencode_controls_test.ts" "$stage/$native_name" "$opencode"

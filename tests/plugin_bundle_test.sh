#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM

make --no-print-directory -C "$root" plugin-bundle
cp "$root/plugins/opencode/dist/managed-bash.js" "$stage/first.js"
make --no-print-directory -C "$root" plugin-bundle
cmp "$stage/first.js" "$root/plugins/opencode/dist/managed-bash.js"
cp "$root/plugins/opencode/dist/managed-bash.js" "$stage/managed-bash.js"

BUNDLE_PATH="$stage/managed-bash.js" bun -e '
const plugin = await import(process.env.BUNDLE_PATH)
if (typeof plugin.ManagedBashPlugin !== "function") {
  throw new TypeError("bundle does not export ManagedBashPlugin")
}
if (!Object.values(plugin).every((value) => typeof value === "function")) {
  throw new TypeError("bundle exports a value that OpenCode cannot load as a plugin")
}
'

#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM

case $(opencode --version) in
  1.18.*) ;;
  *)
    printf '%s\n' "OpenCode 1.18.x is required" >&2
    exit 1
    ;;
esac

config_root="$stage/home/.config"
plugin_path="$root/plugins/opencode/dist/managed-bash.js"
mkdir -p "$config_root/opencode" "$stage/home/.local/share" "$stage/home/.cache" "$stage/home/.local/state"
PLUGIN_PATH="$plugin_path" CONFIG_PATH="$config_root/opencode/opencode.json" bun -e '
import { pathToFileURL } from "node:url"
await Bun.write(process.env.CONFIG_PATH, `${JSON.stringify({ plugin: [pathToFileURL(process.env.PLUGIN_PATH).href] })}\n`)
'

HOME="$stage/home" \
XDG_CONFIG_HOME="$config_root" \
XDG_DATA_HOME="$stage/home/.local/share" \
XDG_CACHE_HOME="$stage/home/.cache" \
XDG_STATE_HOME="$stage/home/.local/state" \
opencode debug config >"$stage/config.json"

CONFIG_PATH="$stage/config.json" PLUGIN_PATH="$plugin_path" bun -e '
import { pathToFileURL } from "node:url"
const config = await Bun.file(process.env.CONFIG_PATH).json()
const expected = pathToFileURL(process.env.PLUGIN_PATH).href
if (!Array.isArray(config.plugin) || !config.plugin.includes(expected)) {
  throw new TypeError(`OpenCode did not load ${expected}`)
}
'
test ! -e "$config_root/opencode/plugins/managed-bash.js"

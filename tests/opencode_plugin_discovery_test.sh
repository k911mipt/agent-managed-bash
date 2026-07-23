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
plugin_dir="$config_root/opencode/plugins"
mkdir -p "$plugin_dir" "$stage/home/.local/share" "$stage/home/.cache" "$stage/home/.local/state"
ln -s "$root/plugins/opencode/dist/managed-bash.js" "$plugin_dir/managed-bash.js"

HOME="$stage/home" \
XDG_CONFIG_HOME="$config_root" \
XDG_DATA_HOME="$stage/home/.local/share" \
XDG_CACHE_HOME="$stage/home/.cache" \
XDG_STATE_HOME="$stage/home/.local/state" \
opencode debug config >"$stage/config.json"

CONFIG_PATH="$stage/config.json" PLUGIN_PATH="$plugin_dir/managed-bash.js" bun -e '
const config = await Bun.file(process.env.CONFIG_PATH).json()
const expected = `file://${process.env.PLUGIN_PATH}`
if (!Array.isArray(config.plugin) || !config.plugin.includes(expected)) {
  throw new TypeError(`OpenCode did not discover ${expected}`)
}
'

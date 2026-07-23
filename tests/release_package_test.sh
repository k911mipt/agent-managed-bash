#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
: "${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required}"
stage=$(mktemp -d)
stage=$(CDPATH= cd -- "$stage" && pwd -P)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
second="$stage/second"
version=$(tr -d '\r\n' <"$root/VERSION")

make --no-print-directory -C "$root" release-package
mkdir -p "$second"
printf '%s' 'preserve unrelated output' >"$second/keep.txt"
make --no-print-directory -C "$root" release-package DIST_DIR="$second"
test "$(cat "$second/keep.txt")" = 'preserve unrelated output'

for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
	name="agent-managed-bash-$version-$target.tar.gz"
	test -f "$root/dist/$name"
	test -f "$second/$name"
	cmp "$root/dist/$name" "$second/$name"
done

set -- "$root"/dist/*.tar.gz
test "$#" -eq 4

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
binary="$stage/$native_name/bin/managed-bash"
response=$(printf '%s' '{"schema_version":1,"action":"version"}' | "$binary" version)
RESPONSE="$response" VERSION="$version" GOOS="$native_os" GOARCH="$native_arch" bun -e '
const response = JSON.parse(process.env.RESPONSE)
if (!response.ok || response.action !== "version") process.exit(1)
const result = response.result
if (result.product !== "managed-bash" || result.binary_version !== process.env.VERSION || result.protocol_version !== 1 || result.os !== process.env.GOOS || result.architecture !== process.env.GOARCH) process.exit(1)
'
sh "$root/tests/cli_binary_test.sh" "$binary"

install_root="$stage/install"
install_home="$install_root/home"
install_data="$install_root/data"
install_config="$install_root/config"
install_bin="$install_root/bin"
mkdir -p "$install_home" "$install_data" "$install_config" "$install_bin"
install_env="HOME=$install_home XDG_DATA_HOME=$install_data XDG_CONFIG_HOME=$install_config MANAGED_BASH_BIN_DIR=$install_bin"

env $install_env sh "$stage/$native_name/install.sh"
installed="$install_bin/managed-bash"
data_root="$install_data/agent-managed-bash"
test -L "$installed"
test -L "$install_config/opencode/plugins/managed-bash.js"
test "$(readlink "$installed")" = "$data_root/current/bin/managed-bash"
test "$(readlink "$install_config/opencode/plugins/managed-bash.js")" = "$data_root/current/lib/opencode/managed-bash.js"
test "$(readlink "$data_root/current")" = "releases/$version-$native_os-$native_arch"
set -- $(ls -di "$data_root/current")
current_inode=$1
env $install_env sh "$stage/$native_name/install.sh"
set -- $(ls -di "$data_root/current")
test "$current_inode" = "$1"

installed_response=$(printf '%s' '{"schema_version":1,"action":"version"}' | "$installed" version)
test "$installed_response" = "$response"
sh "$root/tests/cli_binary_test.sh" "$installed"

preserved_workspace="$stage/preserved-workspace"
mkdir -p "$preserved_workspace/.managed_bash/jobs/fixture"
printf '%s' 'preserve-this-workspace-state-byte-for-byte' >"$preserved_workspace/.managed_bash/jobs/fixture/state.json"
chmod 700 "$preserved_workspace/.managed_bash" "$preserved_workspace/.managed_bash/jobs" "$preserved_workspace/.managed_bash/jobs/fixture"
chmod 600 "$preserved_workspace/.managed_bash/jobs/fixture/state.json"
tar -cf "$stage/workspace-before.tar" -C "$preserved_workspace" .managed_bash

running_workspace="$stage/running-workspace"
mkdir -p "$running_workspace"
run_request=$(printf '{"schema_version":1,"action":"run","context":{"session_id":"package","workspace_path":"%s","cwd":"%s"},"payload":{"command":"while [ ! -f %s/release ]; do :; done; printf survived"}}' "$running_workspace" "$running_workspace" "$running_workspace")
(
	cd "$running_workspace"
	printf '%s' "$run_request" | env $install_env MANAGED_BASH_HOST_SESSION_ID=package MANAGED_BASH_HOST_WORKSPACE_PATH="$running_workspace" \
		"$installed" run >"$stage/running.stdout"
)
running_job_id=$(OUTPUT_PATH="$stage/running.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (!response.ok || response.result.status !== "running") process.exit(1)
process.stdout.write(response.result.job_id)
')

env $install_env sh "$stage/$native_name/uninstall.sh"
test ! -e "$installed"
test ! -e "$install_config/opencode/plugins/managed-bash.js"
touch "$running_workspace/release"
wait_request=$(printf '{"schema_version":1,"action":"wait","context":{"session_id":"package","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s","timeout_ms":15000,"idle_timeout_ms":15000}}' "$running_workspace" "$running_workspace" "$running_job_id")
(
	cd "$running_workspace"
	printf '%s' "$wait_request" | env $install_env MANAGED_BASH_HOST_SESSION_ID=package MANAGED_BASH_HOST_WORKSPACE_PATH="$running_workspace" \
		"$binary" wait >"$stage/running-wait.stdout"
)
OUTPUT_PATH="$stage/running-wait.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (!response.ok || response.result.observation.job.status !== "succeeded" || response.result.output.text !== "survived") process.exit(1)
'
env $install_env sh "$stage/$native_name/uninstall.sh"
tar -cf "$stage/workspace-after.tar" -C "$preserved_workspace" .managed_bash
cmp "$stage/workspace-before.tar" "$stage/workspace-after.tar"

tampered_parent="$stage/tampered"
mkdir -p "$tampered_parent" "$stage/tampered-home" "$stage/tampered-data" "$stage/tampered-config" "$stage/tampered-bin"
cp -R "$stage/$native_name" "$tampered_parent/$native_name"
chmod 644 "$tampered_parent/$native_name/lib/opencode/managed-bash.js"
printf '%s' 'tampered' >"$tampered_parent/$native_name/lib/opencode/managed-bash.js"
set +e
HOME="$stage/tampered-home" XDG_DATA_HOME="$stage/tampered-data" XDG_CONFIG_HOME="$stage/tampered-config" \
	MANAGED_BASH_BIN_DIR="$stage/tampered-bin" sh "$tampered_parent/$native_name/install.sh" >/dev/null 2>&1
tampered_exit=$?
set -e
test "$tampered_exit" -ne 0
test ! -e "$stage/tampered-bin/managed-bash"

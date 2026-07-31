#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
dispatcher="$root/.opencode/skills/managed-bash/scripts/managed-bash"
command -v tmux >/dev/null 2>&1 || { printf '%s\n' 'portable skill edge tests require tmux on PATH' >&2; exit 1; }
stage=$(mktemp -d)
socket="managed-bash-edge-$$"
trap 'tmux -L "$socket" kill-server >/dev/null 2>&1 || true; rm -rf "$stage"' EXIT HUP INT TERM
workspace="$stage/workspace"
tmux_bin="$stage/tmux-bin"
mkdir -p "$workspace" "$tmux_bin"
for dependency in awk date sed sleep tmux tr uname wc; do
	path=$(command -v "$dependency")
	ln -s "$path" "$tmux_bin/$dependency"
done

run_tmux() {
	(
		cd "$workspace"
		unset MANAGED_BASH_BINARY
		PATH="$tmux_bin" MANAGED_BASH_TMUX_SOCKET="$socket" MANAGED_BASH_SESSION_ID=edge-test \
			"$dispatcher" "$@"
	)
}

validate_response() {
	RESPONSE_PATH=$1 SCHEMA_ROOT="$root/schemas/v1" bun -e '
import Ajv2020 from "ajv/dist/2020"
const ajv = new Ajv2020({ strict: true })
ajv.addSchema(await Bun.file(`${process.env.SCHEMA_ROOT}/models.schema.json`).json())
const validate = ajv.compile(await Bun.file(`${process.env.SCHEMA_ROOT}/response.schema.json`).json())
const response = JSON.parse(await Bun.file(process.env.RESPONSE_PATH).text())
if (!validate(response)) throw new Error(JSON.stringify(validate.errors))
'
}

assert_nonzero_exit() {
	exit_code=$1
	run_tmux run -- "exit $exit_code" >"$stage/run-$exit_code.json" 2>"$stage/run-$exit_code.stderr"
	job=$(OUTPUT_PATH="$stage/run-$exit_code.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.observation.job.job_id)')
	run_tmux wait "$job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/wait-$exit_code.json" 2>"$stage/wait-$exit_code.stderr"
	validate_response "$stage/wait-$exit_code.json"
	OUTPUT_PATH="$stage/wait-$exit_code.json" EXPECTED_CODE="$exit_code" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (response.result.observation.job.status !== "nonzero_exit") process.exit(1)
if (response.result.observation.process_result.exit_code !== Number(process.env.EXPECTED_CODE)) process.exit(1)
'
	run_tmux remove "$job" >/dev/null 2>"$stage/remove-$exit_code.stderr"
}

assert_nonzero_exit 128
assert_nonzero_exit 193
assert_nonzero_exit 255

run_tmux run --timeout-ms 1500 --idle-timeout-ms 1200 -- "sleep 5" >"$stage/deadline-run.json" 2>"$stage/deadline-run.stderr"
validate_response "$stage/deadline-run.json"
deadline_job=$(OUTPUT_PATH="$stage/deadline-run.json" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (response.result.reason !== "output_idle" || response.result.observation.job.status !== "running") process.exit(1)
process.stdout.write(response.result.observation.job.job_id)
')
run_tmux cancel "$deadline_job" >/dev/null 2>"$stage/deadline-cancel.stderr"
run_tmux wait "$deadline_job" --timeout-ms 5000 --idle-timeout-ms 5000 >/dev/null 2>"$stage/deadline-wait.stderr"
run_tmux remove "$deadline_job" >/dev/null 2>"$stage/deadline-remove.stderr"

run_tmux run --timeout-ms 1200 --idle-timeout-ms 1500 -- "while :; do printf x; sleep 0.1; done" >"$stage/active-run.json" 2>"$stage/active-run.stderr"
validate_response "$stage/active-run.json"
active_job=$(OUTPUT_PATH="$stage/active-run.json" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (response.result.reason !== "observation_timeout" || response.result.observation.job.status !== "running") process.exit(1)
if (response.result.output.text === "") process.exit(1)
process.stdout.write(response.result.observation.job.job_id)
')
run_tmux cancel "$active_job" >/dev/null 2>"$stage/active-cancel.stderr"
run_tmux wait "$active_job" --timeout-ms 5000 --idle-timeout-ms 5000 >/dev/null 2>"$stage/active-wait.stderr"
run_tmux remove "$active_job" >/dev/null 2>"$stage/active-remove.stderr"

snapshot_command="while [ ! -f '$workspace/snapshot-release' ]; do sleep 0.05; done"
run_tmux start -- "$snapshot_command" >"$stage/snapshot-start.json" 2>"$stage/snapshot-start.stderr"
snapshot_job=$(OUTPUT_PATH="$stage/snapshot-start.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.job_id)')
(
	script_dir="$root/.opencode/skills/managed-bash/scripts"
	. "$script_dir/common.sh"
	tmux_binary=$(command -v tmux)
	current_owner=edge-test
	current_workspace=$workspace
	. "$script_dir/tmux-lib.sh"
	capture_job_snapshot "$snapshot_job"
	test "$snapshot_status" = running
	: >"$workspace/snapshot-release"
	retries=0
	while [ "$(job_status "$snapshot_job")" = running ] && [ "$retries" -lt 100 ]; do
		sleep 0.05
		retries=$((retries + 1))
	done
	test "$(job_status "$snapshot_job")" = succeeded
	printf '{"schema_version":1,"ok":true,"action":"wait","result":{"reason":"output_idle","observation":%s,"output":%s}}\n' \
		"$(observation_snapshot_json)" "$(output_snapshot_json)"
) >"$stage/snapshot-transition.json"
validate_response "$stage/snapshot-transition.json"
OUTPUT_PATH="$stage/snapshot-transition.json" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (response.result.observation.job.status !== "running") process.exit(1)
if (response.result.observation.process_result !== undefined || response.result.output.eof !== false) process.exit(1)
'
run_tmux remove "$snapshot_job" >/dev/null 2>"$stage/snapshot-remove.stderr"

(
	script_dir="$root/.opencode/skills/managed-bash/scripts"
	. "$script_dir/common.sh"
	tmux_binary=$(command -v tmux)
	socket=
	current_owner=edge-test
	current_workspace=$workspace
	. "$script_dir/tmux-lib.sh"
	test "$(signal_number TERM)" -eq 15
	test "$(signal_number 9)" -eq 9
)
test ! -e "$workspace/.managed_bash"

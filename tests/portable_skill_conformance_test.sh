#!/bin/sh

set -eu

binary=${1:?managed-bash binary path is required}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
dispatcher="$root/.opencode/skills/managed-bash/scripts/managed-bash"
fixture="$root/fixtures/v1/portable-skill/scenarios.json"
command -v tmux >/dev/null 2>&1 || { printf '%s\n' 'portable skill conformance requires tmux on PATH' >&2; exit 1; }
stage=$(mktemp -d)
socket="managed-bash-conformance-$$"
trap 'tmux -L "$socket" kill-server >/dev/null 2>&1 || true; rm -rf "$stage"' EXIT HUP INT TERM

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

fixture_field() {
	FIXTURE_PATH="$fixture" SCENARIO_NAME=$1 FIELD_NAME=$2 bun -e '
const fixture = await Bun.file(process.env.FIXTURE_PATH).json()
if (fixture.schema_version !== 1) process.exit(1)
const scenario = fixture.scenarios.find(({ name }) => name === process.env.SCENARIO_NAME)
if (scenario === undefined || !(process.env.FIELD_NAME in scenario)) process.exit(1)
process.stdout.write(String(scenario[process.env.FIELD_NAME]))
'
}

command=$(fixture_field 'successful rendered output' command)
expected_status=$(fixture_field 'successful rendered output' expected_status)
expected_output=$(fixture_field 'successful rendered output' expected_output)

cli_workspace="$stage/cli-workspace"
tmux_workspace="$stage/tmux-workspace"
tmux_bin="$stage/tmux-bin"
mkdir -p "$cli_workspace" "$tmux_workspace" "$tmux_bin"
for dependency in awk date sed sleep tmux tr uname wc; do
	path=$(command -v "$dependency")
	ln -s "$path" "$tmux_bin/$dependency"
done

run_cli() {
	(
		cd "$cli_workspace"
		MANAGED_BASH_BINARY="$binary" MANAGED_BASH_SESSION_ID=portable-conformance \
			MANAGED_BASH_WORKSPACE_PATH="$cli_workspace" "$dispatcher" "$@"
	)
}

run_tmux() {
	(
		cd "$tmux_workspace"
		unset MANAGED_BASH_BINARY
		PATH="$tmux_bin" MANAGED_BASH_TMUX_SOCKET="$socket" MANAGED_BASH_SESSION_ID=portable-conformance \
			"$dispatcher" "$@"
	)
}

run_cli run -- "$command" >"$stage/cli-run.json" 2>"$stage/cli-run.stderr"
validate_response "$stage/cli-run.json"
cli_job=$(OUTPUT_PATH="$stage/cli-run.json" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (!response.ok || response.action !== "run") process.exit(1)
process.stdout.write(response.result.job_id)
')
run_cli wait "$cli_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/cli-wait.json" 2>"$stage/cli-wait.stderr"
validate_response "$stage/cli-wait.json"

run_tmux run -- "$command" >"$stage/tmux-run.json" 2>"$stage/tmux-run.stderr"
validate_response "$stage/tmux-run.json"
tmux_job=$(OUTPUT_PATH="$stage/tmux-run.json" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (!response.ok || response.action !== "run") process.exit(1)
process.stdout.write(response.result.job_id)
')
run_tmux wait "$tmux_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/tmux-wait.json" 2>"$stage/tmux-wait.stderr"
validate_response "$stage/tmux-wait.json"

CLI_PATH="$stage/cli-wait.json" TMUX_PATH="$stage/tmux-wait.json" EXPECTED_STATUS="$expected_status" EXPECTED_OUTPUT="$expected_output" bun -e '
const normalize = async (path) => {
  const response = JSON.parse(await Bun.file(path).text())
  if (!response.ok || response.action !== "wait") process.exit(1)
  return {
    status: response.result.observation.job.status,
    output: response.result.output.text,
  }
}
const [cli, tmux] = await Promise.all([normalize(process.env.CLI_PATH), normalize(process.env.TMUX_PATH)])
const expected = { status: process.env.EXPECTED_STATUS, output: process.env.EXPECTED_OUTPUT }
if (JSON.stringify(cli) !== JSON.stringify(expected)) throw new Error(`CLI mismatch: ${JSON.stringify({ cli, expected })}`)
if (JSON.stringify(tmux) !== JSON.stringify(expected)) throw new Error(`tmux mismatch: ${JSON.stringify({ tmux, expected })}`)
'

test -d "$cli_workspace/.managed_bash/jobs"
test ! -e "$tmux_workspace/.managed_bash"

run_cli status "$cli_job" >"$stage/cli-status.json" 2>"$stage/cli-status.stderr"
run_tmux status "$tmux_job" >"$stage/tmux-status.json" 2>"$stage/tmux-status.stderr"
run_cli output "$cli_job" >"$stage/cli-output.json" 2>"$stage/cli-output.stderr"
run_tmux output "$tmux_job" >"$stage/tmux-output.json" 2>"$stage/tmux-output.stderr"
run_cli list >"$stage/cli-list.json" 2>"$stage/cli-list.stderr"
run_tmux list >"$stage/tmux-list.json" 2>"$stage/tmux-list.stderr"
run_cli version >"$stage/cli-version.json" 2>"$stage/cli-version.stderr"
run_tmux version >"$stage/tmux-version.json" 2>"$stage/tmux-version.stderr"
for response in cli-status tmux-status cli-output tmux-output cli-list tmux-list cli-version tmux-version; do
	validate_response "$stage/$response.json"
done

CLI_STATUS="$stage/cli-status.json" TMUX_STATUS="$stage/tmux-status.json" CLI_OUTPUT="$stage/cli-output.json" TMUX_OUTPUT="$stage/tmux-output.json" CLI_LIST="$stage/cli-list.json" TMUX_LIST="$stage/tmux-list.json" bun -e '
const read = async (name) => JSON.parse(await Bun.file(process.env[name]).text())
const [cliStatus, tmuxStatus, cliOutput, tmuxOutput, cliList, tmuxList] = await Promise.all(
  ["CLI_STATUS", "TMUX_STATUS", "CLI_OUTPUT", "TMUX_OUTPUT", "CLI_LIST", "TMUX_LIST"].map(read),
)
if (cliStatus.result.job.status !== tmuxStatus.result.job.status) process.exit(1)
if (cliOutput.result.output.text !== tmuxOutput.result.output.text) process.exit(1)
if (cliList.result.jobs.length !== 1 || tmuxList.result.jobs.length !== 1) process.exit(1)
'

run_cli cancel "$cli_job" >"$stage/cli-terminal-cancel.json" 2>"$stage/cli-terminal-cancel.stderr"
run_tmux cancel "$tmux_job" >"$stage/tmux-terminal-cancel.json" 2>"$stage/tmux-terminal-cancel.stderr"
validate_response "$stage/cli-terminal-cancel.json"
validate_response "$stage/tmux-terminal-cancel.json"
CLI_PATH="$stage/cli-terminal-cancel.json" TMUX_PATH="$stage/tmux-terminal-cancel.json" bun -e '
const cli = JSON.parse(await Bun.file(process.env.CLI_PATH).text())
const tmux = JSON.parse(await Bun.file(process.env.TMUX_PATH).text())
if (cli.result.outcome !== "already_terminal" || tmux.result.outcome !== "already_terminal") process.exit(1)
'

run_cli remove "$cli_job" >"$stage/cli-remove.json" 2>"$stage/cli-remove.stderr"
run_tmux remove "$tmux_job" >"$stage/tmux-remove.json" 2>"$stage/tmux-remove.stderr"
validate_response "$stage/cli-remove.json"
validate_response "$stage/tmux-remove.json"

nonzero_command=$(fixture_field 'nonzero exit' command)
nonzero_status=$(fixture_field 'nonzero exit' expected_status)
nonzero_code=$(fixture_field 'nonzero exit' expected_exit_code)
run_cli run -- "$nonzero_command" >"$stage/cli-nonzero-run.json" 2>"$stage/cli-nonzero-run.stderr"
run_tmux run -- "$nonzero_command" >"$stage/tmux-nonzero-run.json" 2>"$stage/tmux-nonzero-run.stderr"
cli_nonzero_job=$(OUTPUT_PATH="$stage/cli-nonzero-run.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.job_id)')
tmux_nonzero_job=$(OUTPUT_PATH="$stage/tmux-nonzero-run.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.job_id)')
run_cli wait "$cli_nonzero_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/cli-nonzero-wait.json" 2>"$stage/cli-nonzero-wait.stderr"
run_tmux wait "$tmux_nonzero_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/tmux-nonzero-wait.json" 2>"$stage/tmux-nonzero-wait.stderr"
validate_response "$stage/cli-nonzero-wait.json"
validate_response "$stage/tmux-nonzero-wait.json"
CLI_PATH="$stage/cli-nonzero-wait.json" TMUX_PATH="$stage/tmux-nonzero-wait.json" EXPECTED_STATUS="$nonzero_status" EXPECTED_CODE="$nonzero_code" bun -e '
const values = await Promise.all([process.env.CLI_PATH, process.env.TMUX_PATH].map(async (path) => JSON.parse(await Bun.file(path).text())))
for (const response of values) {
  if (response.result.observation.job.status !== process.env.EXPECTED_STATUS) process.exit(1)
  if (response.result.observation.process_result.exit_code !== Number(process.env.EXPECTED_CODE)) process.exit(1)
}
'
run_cli remove "$cli_nonzero_job" >/dev/null 2>"$stage/cli-nonzero-remove.stderr"
run_tmux remove "$tmux_nonzero_job" >/dev/null 2>"$stage/tmux-nonzero-remove.stderr"

signal_command=$(fixture_field 'signal exit' command)
signal_status=$(fixture_field 'signal exit' expected_status)
signal_number=$(fixture_field 'signal exit' expected_signal)
run_cli run -- "$signal_command" >"$stage/cli-signal-run.json" 2>"$stage/cli-signal-run.stderr"
run_tmux run -- "$signal_command" >"$stage/tmux-signal-run.json" 2>"$stage/tmux-signal-run.stderr"
cli_signal_job=$(OUTPUT_PATH="$stage/cli-signal-run.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.job_id)')
tmux_signal_job=$(OUTPUT_PATH="$stage/tmux-signal-run.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.job_id)')
run_cli wait "$cli_signal_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/cli-signal-wait.json" 2>"$stage/cli-signal-wait.stderr"
run_tmux wait "$tmux_signal_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/tmux-signal-wait.json" 2>"$stage/tmux-signal-wait.stderr"
validate_response "$stage/cli-signal-wait.json"
validate_response "$stage/tmux-signal-wait.json"
CLI_PATH="$stage/cli-signal-wait.json" TMUX_PATH="$stage/tmux-signal-wait.json" EXPECTED_STATUS="$signal_status" EXPECTED_SIGNAL="$signal_number" bun -e '
const values = await Promise.all([process.env.CLI_PATH, process.env.TMUX_PATH].map(async (path) => JSON.parse(await Bun.file(path).text())))
for (const response of values) {
  if (response.result.observation.job.status !== process.env.EXPECTED_STATUS) process.exit(1)
  if (response.result.observation.process_result.signal !== Number(process.env.EXPECTED_SIGNAL)) process.exit(1)
}
'
run_cli remove "$cli_signal_job" >/dev/null 2>"$stage/cli-signal-remove.stderr"
run_tmux remove "$tmux_signal_job" >/dev/null 2>"$stage/tmux-signal-remove.stderr"

cancel_command=$(fixture_field cancellation command)
cancel_status=$(fixture_field cancellation expected_status)
run_cli run --hard-timeout-ms 10000 -- "$cancel_command" >"$stage/cli-cancel-run.json" 2>"$stage/cli-cancel-run.stderr"
run_tmux run --hard-timeout-ms 10000 -- "$cancel_command" >"$stage/tmux-cancel-run.json" 2>"$stage/tmux-cancel-run.stderr"
cli_cancel_job=$(OUTPUT_PATH="$stage/cli-cancel-run.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.job_id)')
tmux_cancel_job=$(OUTPUT_PATH="$stage/tmux-cancel-run.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.job_id)')
set +e
run_cli remove "$cli_cancel_job" >"$stage/cli-active-remove.json" 2>"$stage/cli-active-remove.stderr"
cli_active_remove_exit=$?
run_tmux remove "$tmux_cancel_job" >"$stage/tmux-active-remove.json" 2>"$stage/tmux-active-remove.stderr"
tmux_active_remove_exit=$?
set -e
test "$cli_active_remove_exit" -eq 4
test "$tmux_active_remove_exit" -eq 4
validate_response "$stage/cli-active-remove.json"
run_cli cancel "$cli_cancel_job" >"$stage/cli-cancel.json" 2>"$stage/cli-cancel.stderr"
run_tmux cancel "$tmux_cancel_job" >"$stage/tmux-cancel.json" 2>"$stage/tmux-cancel.stderr"
run_cli cancel "$cli_cancel_job" >"$stage/cli-cancel-repeat.json" 2>"$stage/cli-cancel-repeat.stderr"
run_tmux cancel "$tmux_cancel_job" >"$stage/tmux-cancel-repeat.json" 2>"$stage/tmux-cancel-repeat.stderr"
for response in cli-cancel tmux-cancel cli-cancel-repeat tmux-cancel-repeat; do validate_response "$stage/$response.json"; done
CLI_FIRST="$stage/cli-cancel.json" TMUX_FIRST="$stage/tmux-cancel.json" CLI_SECOND="$stage/cli-cancel-repeat.json" TMUX_SECOND="$stage/tmux-cancel-repeat.json" bun -e '
const read = async (name) => JSON.parse(await Bun.file(process.env[name]).text())
const [cliFirst, tmuxFirst, cliSecond, tmuxSecond] = await Promise.all(["CLI_FIRST", "TMUX_FIRST", "CLI_SECOND", "TMUX_SECOND"].map(read))
if (cliFirst.result.outcome !== "requested" || tmuxFirst.result.outcome !== "requested") process.exit(1)
if (cliSecond.result.outcome !== "already_requested" || tmuxSecond.result.outcome !== "already_requested") process.exit(1)
'
run_cli wait "$cli_cancel_job" --timeout-ms 15000 --idle-timeout-ms 15000 >"$stage/cli-cancel-wait.json" 2>"$stage/cli-cancel-wait.stderr"
run_tmux wait "$tmux_cancel_job" --timeout-ms 15000 --idle-timeout-ms 15000 >"$stage/tmux-cancel-wait.json" 2>"$stage/tmux-cancel-wait.stderr"
validate_response "$stage/cli-cancel-wait.json"
validate_response "$stage/tmux-cancel-wait.json"
CLI_PATH="$stage/cli-cancel-wait.json" TMUX_PATH="$stage/tmux-cancel-wait.json" EXPECTED_STATUS="$cancel_status" bun -e '
const values = await Promise.all([process.env.CLI_PATH, process.env.TMUX_PATH].map(async (path) => JSON.parse(await Bun.file(path).text())))
if (values.some((response) => response.result.observation.job.status !== process.env.EXPECTED_STATUS)) {
  throw new Error(JSON.stringify(values.map((response) => response.result.observation.job.status)))
}
'
run_cli remove "$cli_cancel_job" >/dev/null 2>"$stage/cli-cancel-remove.stderr"
run_tmux remove "$tmux_cancel_job" >/dev/null 2>"$stage/tmux-cancel-remove.stderr"

timeout_command=$(fixture_field 'hard timeout' command)
timeout_ms=$(fixture_field 'hard timeout' hard_timeout_ms)
timeout_status=$(fixture_field 'hard timeout' expected_status)
run_cli run --hard-timeout-ms "$timeout_ms" -- "$timeout_command" >"$stage/cli-timeout-run.json" 2>"$stage/cli-timeout-run.stderr"
run_tmux run --hard-timeout-ms "$timeout_ms" -- "$timeout_command" >"$stage/tmux-timeout-run.json" 2>"$stage/tmux-timeout-run.stderr"
cli_timeout_job=$(OUTPUT_PATH="$stage/cli-timeout-run.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.job_id)')
tmux_timeout_job=$(OUTPUT_PATH="$stage/tmux-timeout-run.json" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.job_id)')
run_cli wait "$cli_timeout_job" --timeout-ms 20000 --idle-timeout-ms 20000 >"$stage/cli-timeout-wait.json" 2>"$stage/cli-timeout-wait.stderr"
run_tmux wait "$tmux_timeout_job" --timeout-ms 20000 --idle-timeout-ms 20000 >"$stage/tmux-timeout-wait.json" 2>"$stage/tmux-timeout-wait.stderr"
validate_response "$stage/cli-timeout-wait.json"
validate_response "$stage/tmux-timeout-wait.json"
CLI_PATH="$stage/cli-timeout-wait.json" TMUX_PATH="$stage/tmux-timeout-wait.json" EXPECTED_STATUS="$timeout_status" bun -e '
const values = await Promise.all([process.env.CLI_PATH, process.env.TMUX_PATH].map(async (path) => JSON.parse(await Bun.file(path).text())))
if (values.some((response) => response.result.observation.job.status !== process.env.EXPECTED_STATUS)) {
  throw new Error(JSON.stringify(values.map((response) => response.result.observation.job.status)))
}
'
run_cli remove "$cli_timeout_job" >/dev/null 2>"$stage/cli-timeout-remove.stderr"
run_tmux remove "$tmux_timeout_job" >/dev/null 2>"$stage/tmux-timeout-remove.stderr"

#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
skill="$root/.opencode/skills/managed-bash"
dispatcher="$skill/scripts/managed-bash"
test -x "$dispatcher"
command -v tmux >/dev/null 2>&1 || { printf '%s\n' 'portable skill tests require tmux on PATH' >&2; exit 1; }
"$dispatcher" --help >"${TMPDIR:-/tmp}/managed-bash-help-$$"
grep -F 'start|run|wait|status|output|cancel|remove|list|version' "${TMPDIR:-/tmp}/managed-bash-help-$$" >/dev/null
rm -f "${TMPDIR:-/tmp}/managed-bash-help-$$"

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

stage=$(mktemp -d)
socket="managed-bash-test-$$"
trap 'tmux -L "$socket" kill-server >/dev/null 2>&1 || true; rm -rf "$stage"' EXIT HUP INT TERM

fake_bin="$stage/fake-bin"
tmux_bin="$stage/tmux-bin"
mkdir -p "$fake_bin" "$tmux_bin"

cat >"$fake_bin/managed-bash" <<'EOF'
#!/bin/sh
set -eu
cat >"$FAKE_CLI_REQUEST"
printf '%s\n' '{"schema_version":1,"ok":true,"action":"version","result":{"product":"managed-bash","binary_version":"fixture","protocol_version":1,"os":"linux","architecture":"amd64"}}'
EOF
chmod 755 "$fake_bin/managed-bash"

FAKE_CLI_REQUEST="$stage/cli-request.json" PATH="$fake_bin:$PATH" \
	"$dispatcher" version >"$stage/cli.stdout" 2>"$stage/cli.stderr"
test "$(cat "$stage/cli.stdout")" = '{"schema_version":1,"ok":true,"action":"version","result":{"product":"managed-bash","binary_version":"fixture","protocol_version":1,"os":"linux","architecture":"amd64"}}'
validate_response "$stage/cli.stdout"
grep -F 'backend=cli' "$stage/cli.stderr" >/dev/null
test "$(cat "$stage/cli-request.json")" = '{"schema_version":1,"action":"version"}'

complex_command='printf '\''line one\nline "two"'\'''
FAKE_CLI_REQUEST="$stage/cli-run-request.json" COMPLEX_COMMAND="$complex_command" PATH="$fake_bin:$PATH" \
	"$dispatcher" run --timeout-ms 300 --idle-timeout-ms 100 -- "$complex_command" >"$stage/cli-run.stdout" 2>"$stage/cli-run.stderr"
REQUEST_PATH="$stage/cli-run-request.json" COMPLEX_COMMAND="$complex_command" bun -e '
const request = JSON.parse(await Bun.file(process.env.REQUEST_PATH).text())
if (request.action !== "run" || request.payload.command !== process.env.COMPLEX_COMMAND) process.exit(1)
if (request.payload.timeout_ms !== 300 || request.payload.idle_timeout_ms !== 100) process.exit(1)
'

for command in awk cat date dirname grep mktemp rm sed sleep tmux tr uname wc; do
	path=$(command -v "$command")
	ln -s "$path" "$tmux_bin/$command"
done

workspace="$stage/workspace"
mkdir -p "$workspace"

run_tmux() {
	(
		cd "$workspace"
		unset MANAGED_BASH_BINARY
		PATH="$tmux_bin" MANAGED_BASH_TMUX_SOCKET="$socket" MANAGED_BASH_SESSION_ID=portable-test \
			"$dispatcher" "$@"
	)
}

run_tmux_as() {
	directory=$1
	owner=$2
	shift 2
	(
		cd "$directory"
		unset MANAGED_BASH_BINARY
		PATH="$tmux_bin" MANAGED_BASH_TMUX_SOCKET="$socket" MANAGED_BASH_SESSION_ID="$owner" \
			"$dispatcher" "$@"
	)
}

run_tmux run --hard-timeout-ms 5000 --timeout-ms 5000 --idle-timeout-ms 5000 -- "printf 'portable-output\\n'" >"$stage/run.stdout" 2>"$stage/run.stderr"
validate_response "$stage/run.stdout"
grep -F 'backend=tmux' "$stage/run.stderr" >/dev/null
job_id=$(OUTPUT_PATH="$stage/run.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (!response.ok || response.action !== "run") process.exit(1)
if (response.result.reason !== "terminal" || response.result.observation.job.status !== "succeeded") process.exit(1)
if (!response.result.output.text.includes("portable-output")) process.exit(1)
process.stdout.write(response.result.observation.job.job_id)
')

run_tmux wait "$job_id" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/wait.stdout" 2>"$stage/wait.stderr"
validate_response "$stage/wait.stdout"
OUTPUT_PATH="$stage/wait.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (!response.ok || response.action !== "wait") process.exit(1)
if (response.result.observation.job.status !== "succeeded") process.exit(1)
if (!response.result.output.text.includes("portable-output")) process.exit(1)
'

run_tmux status "$job_id" >"$stage/status.stdout" 2>"$stage/status.stderr"
run_tmux output "$job_id" >"$stage/output.stdout" 2>"$stage/output.stderr"
run_tmux list >"$stage/list.stdout" 2>"$stage/list.stderr"
validate_response "$stage/status.stdout"
validate_response "$stage/output.stdout"
validate_response "$stage/list.stdout"
JOB_ID="$job_id" STATUS_PATH="$stage/status.stdout" OUTPUT_PATH="$stage/output.stdout" LIST_PATH="$stage/list.stdout" bun -e '
const status = JSON.parse(await Bun.file(process.env.STATUS_PATH).text())
const output = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
const list = JSON.parse(await Bun.file(process.env.LIST_PATH).text())
if (!status.ok || status.result.job.status !== "succeeded") process.exit(1)
if (!output.ok || !output.result.output.text.includes("portable-output")) process.exit(1)
if (!list.ok || list.result.jobs.length !== 1 || list.result.jobs[0].job_id !== process.env.JOB_ID) process.exit(1)
'
test ! -e "$workspace/.managed_bash"

set +e
run_tmux output "$job_id" --start-cursor-bytes 1 >"$stage/range.stdout" 2>"$stage/range.stderr"
range_exit=$?
run_tmux run --output-limit-bytes 1024 -- 'printf unsupported' >"$stage/limit.stdout" 2>"$stage/limit.stderr"
limit_exit=$?
set -e
test "$range_exit" -eq 2
test "$limit_exit" -eq 2
grep -F 'unsupported by the tmux backend' "$stage/range.stderr" >/dev/null
grep -F 'unsupported by the tmux backend' "$stage/limit.stderr" >/dev/null

run_tmux cancel "$job_id" >"$stage/terminal-cancel.stdout" 2>"$stage/terminal-cancel.stderr"
validate_response "$stage/terminal-cancel.stdout"
OUTPUT_PATH="$stage/terminal-cancel.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (response.result.outcome !== "already_terminal" || response.result.cancellation !== undefined) process.exit(1)
'

run_tmux remove "$job_id" >"$stage/remove.stdout" 2>"$stage/remove.stderr"
validate_response "$stage/remove.stdout"
run_tmux list >"$stage/empty-list.stdout" 2>"$stage/empty-list.stderr"
validate_response "$stage/empty-list.stdout"
OUTPUT_PATH="$stage/empty-list.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (!response.ok || response.result.jobs.length !== 0) process.exit(1)
'

run_tmux start --hard-timeout-ms 10000 -- 'sleep 30' >"$stage/cancel-run.stdout" 2>"$stage/cancel-run.stderr"
cancel_job=$(OUTPUT_PATH="$stage/cancel-run.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
process.stdout.write(response.result.job_id)
')
other_workspace="$stage/other-workspace"
mkdir -p "$other_workspace"
set +e
run_tmux_as "$other_workspace" portable-test status "$cancel_job" >"$stage/cross-workspace.stdout" 2>"$stage/cross-workspace.stderr"
cross_workspace_exit=$?
run_tmux_as "$workspace" other-owner cancel "$cancel_job" >"$stage/cross-owner.stdout" 2>"$stage/cross-owner.stderr"
cross_owner_exit=$?
set -e
test "$cross_workspace_exit" -eq 3
test "$cross_owner_exit" -eq 3
run_tmux_as "$other_workspace" portable-test list >"$stage/cross-list.stdout" 2>"$stage/cross-list.stderr"
validate_response "$stage/cross-list.stdout"
OUTPUT_PATH="$stage/cross-list.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (response.result.jobs.length !== 0) process.exit(1)
'
run_tmux cancel "$cancel_job" >"$stage/cancel.stdout" 2>"$stage/cancel.stderr"
validate_response "$stage/cancel.stdout"
run_tmux cancel "$cancel_job" >"$stage/repeated-cancel.stdout" 2>"$stage/repeated-cancel.stderr"
validate_response "$stage/repeated-cancel.stdout"
FIRST_PATH="$stage/cancel.stdout" SECOND_PATH="$stage/repeated-cancel.stdout" bun -e '
const first = JSON.parse(await Bun.file(process.env.FIRST_PATH).text())
const second = JSON.parse(await Bun.file(process.env.SECOND_PATH).text())
if (first.result.outcome !== "requested" || second.result.outcome !== "already_requested") process.exit(1)
if (JSON.stringify(first.result.cancellation) !== JSON.stringify(second.result.cancellation)) process.exit(1)
'
run_tmux wait "$cancel_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/cancel-wait.stdout" 2>"$stage/cancel-wait.stderr"
validate_response "$stage/cancel-wait.stdout"
OUTPUT_PATH="$stage/cancel-wait.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (!response.ok || response.result.observation.job.status !== "cancelled") process.exit(1)
'
run_tmux remove "$cancel_job" >/dev/null 2>"$stage/cancel-remove.stderr"

run_tmux start --hard-timeout-ms 1000 -- 'sleep 30' >"$stage/timeout-run.stdout" 2>"$stage/timeout-run.stderr"
timeout_job=$(OUTPUT_PATH="$stage/timeout-run.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
process.stdout.write(response.result.job_id)
')
run_tmux wait "$timeout_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/timeout-wait.stdout" 2>"$stage/timeout-wait.stderr"
validate_response "$stage/timeout-wait.stdout"
OUTPUT_PATH="$stage/timeout-wait.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (!response.ok || response.result.observation.job.status !== "hard_timeout") process.exit(1)
'
run_tmux remove "$timeout_job" >/dev/null 2>"$stage/timeout-remove.stderr"
test ! -e "$workspace/.managed_bash"

run_tmux run -- 'exit 7' >"$stage/nonzero-run.stdout" 2>"$stage/nonzero-run.stderr"
nonzero_job=$(OUTPUT_PATH="$stage/nonzero-run.stdout" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.observation.job.job_id)')
run_tmux wait "$nonzero_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/nonzero-wait.stdout" 2>"$stage/nonzero-wait.stderr"
validate_response "$stage/nonzero-wait.stdout"
OUTPUT_PATH="$stage/nonzero-wait.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (response.result.observation.job.status !== "nonzero_exit" || response.result.observation.process_result.exit_code !== 7) process.exit(1)
'
run_tmux remove "$nonzero_job" >/dev/null 2>"$stage/nonzero-remove.stderr"

run_tmux run -- 'kill -TERM $$' >"$stage/signal-run.stdout" 2>"$stage/signal-run.stderr"
signal_job=$(OUTPUT_PATH="$stage/signal-run.stdout" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.observation.job.job_id)')
run_tmux wait "$signal_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/signal-wait.stdout" 2>"$stage/signal-wait.stderr"
validate_response "$stage/signal-wait.stdout"
OUTPUT_PATH="$stage/signal-wait.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (response.result.observation.job.status !== "signal_exit") process.exit(1)
if (response.result.observation.process_result.signal !== 15) process.exit(1)
'
run_tmux remove "$signal_job" >/dev/null 2>"$stage/signal-remove.stderr"

run_tmux run -- "printf 'Pane is dead (status 0, command output)'" >"$stage/banner-run.stdout" 2>"$stage/banner-run.stderr"
banner_job=$(OUTPUT_PATH="$stage/banner-run.stdout" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.observation.job.job_id)')
run_tmux wait "$banner_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/banner-wait.stdout" 2>"$stage/banner-wait.stderr"
OUTPUT_PATH="$stage/banner-wait.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
if (response.result.output.text !== "Pane is dead (status 0, command output)") process.exit(1)
'
run_tmux remove "$banner_job" >/dev/null 2>"$stage/banner-remove.stderr"

large_command='index=1; while [ "$index" -le 260 ]; do printf "line-%03d\n" "$index"; index=$((index + 1)); done'
run_tmux run -- "$large_command" >"$stage/large-run.stdout" 2>"$stage/large-run.stderr"
large_job=$(OUTPUT_PATH="$stage/large-run.stdout" bun -e 'process.stdout.write(JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text()).result.observation.job.job_id)')
run_tmux wait "$large_job" --timeout-ms 5000 --idle-timeout-ms 5000 >"$stage/large-wait.stdout" 2>"$stage/large-wait.stderr"
validate_response "$stage/large-wait.stdout"
OUTPUT_PATH="$stage/large-wait.stdout" bun -e '
const response = JSON.parse(await Bun.file(process.env.OUTPUT_PATH).text())
const lines = response.result.output.text.split("\n")
if (lines.length > 200 || lines.includes("line-001") || !lines.includes("line-260")) process.exit(1)
'
run_tmux remove "$large_job" >/dev/null 2>"$stage/large-remove.stderr"

run_tmux version >"$stage/tmux-version.stdout" 2>"$stage/tmux-version.stderr"
validate_response "$stage/tmux-version.stdout"

control_command=$(printf 'printf \001')
oversized_command=$(bun -e 'process.stdout.write("x".repeat(65537))')
set +e
run_tmux run --hard-timeout-ms 0 -- 'printf invalid' >"$stage/zero-timeout.stdout" 2>"$stage/zero-timeout.stderr"
zero_timeout_exit=$?
run_tmux run --hard-timeout-ms 86400001 -- 'printf invalid' >"$stage/large-timeout.stdout" 2>"$stage/large-timeout.stderr"
large_timeout_exit=$?
run_tmux run -- "$control_command" >"$stage/control.stdout" 2>"$stage/control.stderr"
control_exit=$?
run_tmux run -- "$oversized_command" >"$stage/oversized.stdout" 2>"$stage/oversized.stderr"
oversized_exit=$?
set -e
test "$zero_timeout_exit" -eq 2
test "$large_timeout_exit" -eq 2
test "$control_exit" -eq 2
test "$oversized_exit" -eq 2

empty_path="$stage/empty-path"
mkdir -p "$empty_path"
set +e
(unset MANAGED_BASH_BINARY; PATH="$empty_path" "$dispatcher" version) >"$stage/missing.stdout" 2>"$stage/missing.stderr"
missing_exit=$?
set -e
test "$missing_exit" -eq 127
grep -F 'install tmux or managed-bash' "$stage/missing.stderr" >/dev/null

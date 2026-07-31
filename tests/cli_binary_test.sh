#!/bin/sh

set -eu

binary=${1:?managed-bash binary path is required}
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT HUP INT TERM
canonical_temp_dir=$(cd "$temp_dir" && pwd -P)
temp_dir=$canonical_temp_dir
workspace="$temp_dir/workspace"
mkdir -p "$workspace"

"$binary" --help >"$temp_dir/help.stdout" 2>"$temp_dir/help.stderr"
test ! -s "$temp_dir/help.stderr"
test "$(wc -l <"$temp_dir/help.stdout")" -eq 1

printf '%s' '{"schema_version":1,"action":"version"}' |
	"$binary" version >"$temp_dir/version.stdout" 2>"$temp_dir/version.stderr"
test ! -s "$temp_dir/version.stderr"
OUTPUT_PATH="$temp_dir/version.stdout" bun -e '
const path = process.env.OUTPUT_PATH
if (!path) throw new Error("OUTPUT_PATH is required")
const response = JSON.parse(await Bun.file(path).text())
if (!response.ok || response.action !== "version" || response.result.product !== "managed-bash" || response.result.protocol_version !== 1) process.exit(1)
'

run_request=$(printf '{"schema_version":1,"action":"run","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"command":"printf acceptance"}}' "$workspace" "$workspace")
(
	cd "$workspace"
	printf '%s' "$run_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" run >"$temp_dir/run.stdout" 2>"$temp_dir/run.stderr"
)
test ! -s "$temp_dir/run.stderr"
job_id=$(OUTPUT_PATH="$temp_dir/run.stdout" bun -e '
const path = process.env.OUTPUT_PATH
if (!path) throw new Error("OUTPUT_PATH is required")
const response = JSON.parse(await Bun.file(path).text())
if (!response.ok || response.action !== "run" || response.result.reason !== "terminal") process.exit(1)
if (response.result.observation.job.status !== "succeeded" || response.result.output.text !== "acceptance" || !response.result.output.eof) process.exit(1)
process.stdout.write(response.result.observation.job.job_id)
')

wait_request=$(printf '{"schema_version":1,"action":"wait","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s","timeout_ms":5000,"idle_timeout_ms":5000}}' "$workspace" "$workspace" "$job_id")
(
	cd "$workspace"
	printf '%s' "$wait_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" wait >"$temp_dir/wait.stdout" 2>"$temp_dir/wait.stderr"
)
test ! -s "$temp_dir/wait.stderr"
OUTPUT_PATH="$temp_dir/wait.stdout" bun -e '
const path = process.env.OUTPUT_PATH
if (!path) throw new Error("OUTPUT_PATH is required")
const response = JSON.parse(await Bun.file(path).text())
if (!response.ok || response.action !== "wait" || response.result.reason !== "terminal") process.exit(1)
if (response.result.observation.job.status !== "succeeded" || response.result.output.text !== "" || !response.result.output.eof) process.exit(1)
'

status_request=$(printf '{"schema_version":1,"action":"status","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s"}}' "$workspace" "$workspace" "$job_id")
output_request=$(printf '{"schema_version":1,"action":"output","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s","start_cursor_bytes":1,"end_cursor_bytes":4}}' "$workspace" "$workspace" "$job_id")
list_request=$(printf '{"schema_version":1,"action":"list","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"}}' "$workspace" "$workspace")
cancel_request=$(printf '{"schema_version":1,"action":"cancel","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s"}}' "$workspace" "$workspace" "$job_id")
for action in status output list cancel; do
	eval request=\$${action}_request
	(
		cd "$workspace"
		printf '%s' "$request" |
			env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
			"$binary" "$action" >"$temp_dir/$action.stdout" 2>"$temp_dir/$action.stderr"
	)
	test ! -s "$temp_dir/$action.stderr"
done
STATUS_PATH="$temp_dir/status.stdout" OUTPUT_PATH="$temp_dir/output.stdout" LIST_PATH="$temp_dir/list.stdout" CANCEL_PATH="$temp_dir/cancel.stdout" bun -e '
const paths = ["STATUS_PATH", "OUTPUT_PATH", "LIST_PATH", "CANCEL_PATH"].map((name) => process.env[name])
if (paths.some((path) => !path)) throw new Error("action response paths are required")
const [status, output, list, cancel] = await Promise.all(paths.map((path) => Bun.file(path).text().then(JSON.parse)))
if (!status.ok || status.result.job.status !== "succeeded") process.exit(1)
if (!output.ok || output.result.output.text !== "cce") process.exit(1)
if (!list.ok || list.result.jobs.length !== 1 || list.result.jobs[0].job_id !== status.result.job.job_id) process.exit(1)
if (!cancel.ok || cancel.result.outcome !== "already_terminal") process.exit(1)
'

remove_request=$(printf '{"schema_version":1,"action":"remove","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s"}}' "$workspace" "$workspace" "$job_id")
(
	cd "$workspace"
	printf '%s' "$remove_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" remove >"$temp_dir/remove.stdout" 2>"$temp_dir/remove.stderr"
)
test ! -s "$temp_dir/remove.stderr"

missing_request=$(printf '{"schema_version":1,"action":"status","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"job-missing"}}' "$workspace" "$workspace")
set +e
(
	cd "$workspace"
	printf '%s' "$missing_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" status >"$temp_dir/missing.stdout" 2>"$temp_dir/missing.stderr"
)
missing_exit=$?
set -e
test "$missing_exit" -eq 3

active_run_request=$(printf '{"schema_version":1,"action":"start","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"command":"sleep 30"}}' "$workspace" "$workspace")
(
	cd "$workspace"
	printf '%s' "$active_run_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" start >"$temp_dir/active-run.stdout" 2>"$temp_dir/active-run.stderr"
)
active_job_id=$(OUTPUT_PATH="$temp_dir/active-run.stdout" bun -e '
const path = process.env.OUTPUT_PATH
if (!path) throw new Error("OUTPUT_PATH is required")
process.stdout.write(JSON.parse(await Bun.file(path).text()).result.job_id)
')
active_remove_request=$(printf '{"schema_version":1,"action":"remove","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s"}}' "$workspace" "$workspace" "$active_job_id")
set +e
(
	cd "$workspace"
	printf '%s' "$active_remove_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" remove >"$temp_dir/active-remove.stdout" 2>"$temp_dir/active-remove.stderr"
)
active_remove_exit=$?
set -e
test "$active_remove_exit" -eq 4
active_cancel_request=$(printf '{"schema_version":1,"action":"cancel","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s"}}' "$workspace" "$workspace" "$active_job_id")
(
	cd "$workspace"
	printf '%s' "$active_cancel_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" cancel >"$temp_dir/active-cancel.stdout" 2>"$temp_dir/active-cancel.stderr"
)
active_wait_request=$(printf '{"schema_version":1,"action":"wait","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s","timeout_ms":15000,"idle_timeout_ms":15000}}' "$workspace" "$workspace" "$active_job_id")
(
	cd "$workspace"
	printf '%s' "$active_wait_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" wait >"$temp_dir/active-wait.stdout" 2>"$temp_dir/active-wait.stderr"
)
(
	cd "$workspace"
	printf '%s' "$active_remove_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" remove >"$temp_dir/active-cleanup.stdout" 2>"$temp_dir/active-cleanup.stderr"
)

corrupt_run_request=$(printf '{"schema_version":1,"action":"start","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"command":"printf corrupt"}}' "$workspace" "$workspace")
(
	cd "$workspace"
	printf '%s' "$corrupt_run_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" start >"$temp_dir/corrupt-run.stdout" 2>"$temp_dir/corrupt-run.stderr"
)
corrupt_job_id=$(OUTPUT_PATH="$temp_dir/corrupt-run.stdout" bun -e '
const path = process.env.OUTPUT_PATH
if (!path) throw new Error("OUTPUT_PATH is required")
process.stdout.write(JSON.parse(await Bun.file(path).text()).result.job_id)
')
corrupt_wait_request=$(printf '{"schema_version":1,"action":"wait","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s","timeout_ms":5000,"idle_timeout_ms":5000}}' "$workspace" "$workspace" "$corrupt_job_id")
(
	cd "$workspace"
	printf '%s' "$corrupt_wait_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" wait >"$temp_dir/corrupt-wait.stdout" 2>"$temp_dir/corrupt-wait.stderr"
)
printf '%s' '{}' >"$workspace/.managed_bash/jobs/$corrupt_job_id/state.json"
corrupt_status_request=$(printf '{"schema_version":1,"action":"status","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"},"payload":{"job_id":"%s"}}' "$workspace" "$workspace" "$corrupt_job_id")
set +e
(
	cd "$workspace"
	printf '%s' "$corrupt_status_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace" \
		"$binary" status >"$temp_dir/corrupt-status.stdout" 2>"$temp_dir/corrupt-status.stderr"
)
corrupt_exit=$?
set -e
test "$corrupt_exit" -eq 5

workspace_link="$temp_dir/workspace-link"
ln -s "$workspace" "$workspace_link"
symlink_request=$(printf '{"schema_version":1,"action":"list","context":{"session_id":"acceptance","workspace_path":"%s","cwd":"%s"}}' "$workspace_link" "$workspace")
set +e
(
	cd "$workspace"
	printf '%s' "$symlink_request" |
		env MANAGED_BASH_HOST_SESSION_ID=acceptance MANAGED_BASH_HOST_WORKSPACE_PATH="$workspace_link" \
		"$binary" list >"$temp_dir/symlink.stdout" 2>"$temp_dir/symlink.stderr"
)
symlink_exit=$?
set -e
test "$symlink_exit" -eq 3

set +e
printf '%s' '{"schema_version":1' | "$binary" run >"$temp_dir/malformed.stdout" 2>"$temp_dir/malformed.stderr"
malformed_exit=$?
printf '%s' '{"schema_version":2,"action":"version"}' | "$binary" version >"$temp_dir/incompatible.stdout" 2>"$temp_dir/incompatible.stderr"
incompatible_exit=$?
set -e
test "$malformed_exit" -eq 2
test "$incompatible_exit" -eq 2

MALFORMED_PATH="$temp_dir/malformed.stdout" INCOMPATIBLE_PATH="$temp_dir/incompatible.stdout" bun -e '
const malformedPath = process.env.MALFORMED_PATH
const incompatiblePath = process.env.INCOMPATIBLE_PATH
if (!malformedPath || !incompatiblePath) throw new Error("response paths are required")
const malformed = JSON.parse(await Bun.file(malformedPath).text())
const incompatible = JSON.parse(await Bun.file(incompatiblePath).text())
if (malformed.ok || malformed.error.code !== "malformed_json") process.exit(1)
if (incompatible.ok || incompatible.error.code !== "incompatible_version") process.exit(1)
'

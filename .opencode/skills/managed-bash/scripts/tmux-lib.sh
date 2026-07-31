#!/bin/sh

tmux_command() {
	if [ -n "$socket" ]; then
		"$tmux_binary" -L "$socket" "$@"
	else
		"$tmux_binary" "$@"
	fi
}

job_exists() {
	tmux_command has-session -t "$1" >/dev/null 2>&1 &&
		[ "$(tmux_command show-options -v -t "$1" @managed_bash_backend 2>/dev/null || true)" = tmux ]
}

job_visible() {
	job_exists "$1" && [ "$(job_option "$1" @managed_bash_workspace)" = "$current_workspace" ]
}

require_job() {
	job_visible "$1" || {
		printf 'managed-bash tmux job not found: %s\n' "$1" >&2
		exit 3
	}
}

require_owned_job() {
	require_job "$1"
	[ "$(job_option "$1" @managed_bash_owner)" = "$current_owner" ] || {
		printf 'managed-bash tmux job is owned by another session: %s\n' "$1" >&2
		exit 3
	}
}

job_option() {
	tmux_command show-options -v -t "$1" "$2" 2>/dev/null || true
}

set_job_option() {
	tmux_command set-option -q -t "$1" "$2" "$3"
}

pane_value() {
	tmux_command display-message -p -t "$1" -F "$2"
}

capture_output() {
	remove_banner=false
	if [ "$(pane_value "$1" '#{pane_dead}')" = 1 ] && [ "$(job_option "$1" @managed_bash_legacy_banner)" = true ]; then
		remove_banner=true
	fi
	tmux_command capture-pane -p -S -150 -t "$1" | REMOVE_TMUX_BANNER=$remove_banner awk '
		{ lines[NR] = $0 }
		END {
			last = NR
			while (last > 0 && lines[last] == "") last--
			if (ENVIRON["REMOVE_TMUX_BANNER"] == "true" && lines[last] ~ /^Pane is dead \((status|signal) [A-Za-z0-9]+, .+\)$/) last--
			while (last > 0 && lines[last] == "") last--
			for (line_number = 1; line_number <= last; line_number++) print lines[line_number]
		}'
}

signal_number() {
	raw=$1
	case $raw in
		HUP|SIGHUP) number=1 ;;
		INT|SIGINT) number=2 ;;
		QUIT|SIGQUIT) number=3 ;;
		ILL|SIGILL) number=4 ;;
		TRAP|SIGTRAP) number=5 ;;
		ABRT|SIGABRT) number=6 ;;
		BUS|SIGBUS) number=7 ;;
		FPE|SIGFPE) number=8 ;;
		KILL|SIGKILL) number=9 ;;
		SEGV|SIGSEGV) number=11 ;;
		PIPE|SIGPIPE) number=13 ;;
		ALRM|SIGALRM) number=14 ;;
		TERM|SIGTERM) number=15 ;;
		''|*[!0-9]*) return 1 ;;
		*) number=$raw ;;
	esac
	case $number in ''|*[!0-9]*) return 1 ;; esac
	[ "$number" -ge 1 ] && [ "$number" -le 64 ] || return 1
	printf '%s\n' "$number"
}

job_status() {
	job=$1
	reason=$(job_option "$job" @managed_bash_reason)
	case $reason in
		cancelled|hard_timeout) printf '%s\n' "$reason"; return ;;
	esac
	exit_code=$(job_option "$job" @managed_bash_exit_code)
	if [ -n "$exit_code" ]; then
		if [ "$exit_code" -eq 0 ]; then
			printf '%s\n' succeeded
		elif [ "$exit_code" -ge 129 ] && [ "$exit_code" -le 192 ]; then
			printf '%s\n' signal_exit
		else
			printf '%s\n' nonzero_exit
		fi
		return
	fi
	if [ "$(pane_value "$job" '#{pane_dead}')" = 0 ]; then
		printf '%s\n' running
		return
	fi
	signal=$(pane_value "$job" '#{pane_dead_signal}')
	code=$(pane_value "$job" '#{pane_dead_status}')
	retries=0
	while [ -z "$signal" ] && [ -z "$code" ] && [ "$retries" -lt 100 ]; do
		sleep 0.01
		signal=$(pane_value "$job" '#{pane_dead_signal}')
		code=$(pane_value "$job" '#{pane_dead_status}')
		retries=$((retries + 1))
	done
	if [ -n "$signal" ] && [ "$signal" != 0 ]; then
		printf '%s\n' signal_exit
	elif [ "${code:-1}" = 0 ]; then
		printf '%s\n' succeeded
	else
		printf '%s\n' nonzero_exit
	fi
}

capture_job_snapshot() {
	snapshot_job=$1
	snapshot_status=$(job_status "$snapshot_job")
	snapshot_command=$(job_option "$snapshot_job" @managed_bash_command)
	snapshot_owner=$(job_option "$snapshot_job" @managed_bash_owner)
	snapshot_workspace=$(job_option "$snapshot_job" @managed_bash_workspace)
	snapshot_cwd=$(job_option "$snapshot_job" @managed_bash_cwd)
	snapshot_created=$(job_option "$snapshot_job" @managed_bash_created_ms)
	snapshot_hard_timeout=$(job_option "$snapshot_job" @managed_bash_hard_timeout_ms)
	snapshot_output=$(capture_output "$snapshot_job")
	snapshot_bytes=$(printf '%s' "$snapshot_output" | wc -c | tr -d ' ')
	snapshot_finished=
	snapshot_exit_code=
	snapshot_signal=
	if [ "$snapshot_status" != running ]; then
		snapshot_finished=$(job_option "$snapshot_job" @managed_bash_finished_ms)
		if [ -z "$snapshot_finished" ]; then
			snapshot_finished=$(now_ms)
			set_job_option "$snapshot_job" @managed_bash_finished_ms "$snapshot_finished"
		fi
		case $snapshot_status in
			succeeded|nonzero_exit)
				snapshot_exit_code=$(job_option "$snapshot_job" @managed_bash_exit_code)
				[ -n "$snapshot_exit_code" ] || snapshot_exit_code=$(pane_value "$snapshot_job" '#{pane_dead_status}')
				[ -n "$snapshot_exit_code" ] || snapshot_exit_code=1
				;;
			signal_exit)
				code=$(job_option "$snapshot_job" @managed_bash_exit_code)
				if [ -n "$code" ] && [ "$code" -ge 129 ] && [ "$code" -le 192 ]; then
					snapshot_signal=$((code - 128))
				else
					snapshot_signal=$(pane_value "$snapshot_job" '#{pane_dead_signal}')
				fi
				snapshot_signal=$(signal_number "$snapshot_signal") || snapshot_signal=15
				;;
		esac
	fi
}

job_snapshot_json() {
	printf '{"job_id":%s,"status":%s,"owner_session_id":%s,"workspace_path":%s,"cwd":%s,"command":%s,"created_at_unix_ms":%s,"started_at_unix_ms":%s,"captured_bytes":%s,"hard_timeout_ms":%s,"output_limit_bytes":104857600' \
		"$(json_quote "$snapshot_job")" "$(json_quote "$snapshot_status")" "$(json_quote "$snapshot_owner")" "$(json_quote "$snapshot_workspace")" \
		"$(json_quote "$snapshot_cwd")" "$(json_quote "$snapshot_command")" "$snapshot_created" "$snapshot_created" "$snapshot_bytes" "$snapshot_hard_timeout"
	if [ "$snapshot_status" != running ]; then
		printf ',"finished_at_unix_ms":%s' "$snapshot_finished"
	fi
	printf '}'
}

process_result_snapshot_json() {
	[ "$snapshot_status" != running ] || return 0
	printf '{"status":%s,"finished_at_unix_ms":%s,"captured_bytes":%s' \
		"$(json_quote "$snapshot_status")" "$snapshot_finished" "$snapshot_bytes"
	case $snapshot_status in
		succeeded|nonzero_exit)
			printf ',"exit_code":%s' "$snapshot_exit_code"
			;;
		signal_exit)
			printf ',"signal":%s' "$snapshot_signal"
			;;
	esac
	printf '}'
}

observation_snapshot_json() {
	printf '{"job":%s' "$(job_snapshot_json)"
	if [ "$snapshot_status" != running ]; then
		printf ',"process_result":%s' "$(process_result_snapshot_json)"
	fi
	printf '}'
}

output_snapshot_json() {
	if [ "$snapshot_status" = running ]; then eof=false; else eof=true; fi
	printf '{"text":%s,"start_cursor_bytes":0,"next_cursor_bytes":%s,"captured_bytes":%s,"eof":%s}' \
		"$(json_quote "$snapshot_output")" "$snapshot_bytes" "$snapshot_bytes" "$eof"
}

job_json() {
	capture_job_snapshot "$1"
	job_snapshot_json
}

process_result_json() {
	capture_job_snapshot "$1"
	process_result_snapshot_json
}

observation_json() {
	capture_job_snapshot "$1"
	observation_snapshot_json
}

output_json() {
	capture_job_snapshot "$1"
	output_snapshot_json
}

terminate_job() {
	job=$1
	reason=$2
	set_job_option "$job" @managed_bash_reason "$reason"
	pid=$(pane_value "$job" '#{pane_pid}')
	kill -TERM "$pid" 2>/dev/null || true
	count=0
	while [ "$count" -lt 20 ] && [ "$(pane_value "$job" '#{pane_dead}')" = 0 ]; do
		sleep 0.1
		count=$((count + 1))
	done
	if [ "$(pane_value "$job" '#{pane_dead}')" = 0 ]; then
		kill -KILL "$pid" 2>/dev/null || true
	fi
}

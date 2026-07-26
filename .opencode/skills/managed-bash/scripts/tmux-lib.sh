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

job_json() {
	job=$1
	status=$(job_status "$job")
	command=$(job_option "$job" @managed_bash_command)
	owner=$(job_option "$job" @managed_bash_owner)
	workspace=$(job_option "$job" @managed_bash_workspace)
	cwd=$(job_option "$job" @managed_bash_cwd)
	created=$(job_option "$job" @managed_bash_created_ms)
	hard_timeout=$(job_option "$job" @managed_bash_hard_timeout_ms)
	output=$(capture_output "$job")
	bytes=$(printf '%s' "$output" | wc -c | tr -d ' ')
	printf '{"job_id":%s,"status":%s,"owner_session_id":%s,"workspace_path":%s,"cwd":%s,"command":%s,"created_at_unix_ms":%s,"started_at_unix_ms":%s,"captured_bytes":%s,"hard_timeout_ms":%s,"output_limit_bytes":104857600' \
		"$(json_quote "$job")" "$(json_quote "$status")" "$(json_quote "$owner")" "$(json_quote "$workspace")" \
		"$(json_quote "$cwd")" "$(json_quote "$command")" "$created" "$created" "$bytes" "$hard_timeout"
	if [ "$status" != running ]; then
		finished=$(job_option "$job" @managed_bash_finished_ms)
		if [ -z "$finished" ]; then
			finished=$(now_ms)
			set_job_option "$job" @managed_bash_finished_ms "$finished"
		fi
		printf ',"finished_at_unix_ms":%s' "$finished"
	fi
	printf '}'
}

process_result_json() {
	job=$1
	status=$(job_status "$job")
	[ "$status" != running ] || return 0
	output=$(capture_output "$job")
	bytes=$(printf '%s' "$output" | wc -c | tr -d ' ')
	finished=$(job_option "$job" @managed_bash_finished_ms)
	if [ -z "$finished" ]; then
		finished=$(now_ms)
		set_job_option "$job" @managed_bash_finished_ms "$finished"
	fi
	printf '{"status":%s,"finished_at_unix_ms":%s,"captured_bytes":%s' "$(json_quote "$status")" "$finished" "$bytes"
	case $status in
		succeeded|nonzero_exit)
			code=$(job_option "$job" @managed_bash_exit_code)
			[ -n "$code" ] || code=$(pane_value "$job" '#{pane_dead_status}')
			printf ',"exit_code":%s' "${code:-1}"
			;;
		signal_exit|cancelled|hard_timeout)
			code=$(job_option "$job" @managed_bash_exit_code)
			if [ -n "$code" ] && [ "$code" -ge 129 ] && [ "$code" -le 192 ]; then signal=$((code - 128)); else signal=$(pane_value "$job" '#{pane_dead_signal}'); fi
			if [ "$status" = signal_exit ]; then
				signal=$(signal_number "$signal") || signal=15
				printf ',"signal":%s' "$signal"
			fi
			;;
	esac
	printf '}'
}

observation_json() {
	job=$1
	status=$(job_status "$job")
	printf '{"job":%s' "$(job_json "$job")"
	if [ "$status" != running ]; then
		printf ',"process_result":%s' "$(process_result_json "$job")"
	fi
	printf '}'
}

output_json() {
	job=$1
	output=$(capture_output "$job")
	bytes=$(printf '%s' "$output" | wc -c | tr -d ' ')
	status=$(job_status "$job")
	if [ "$status" = running ]; then eof=false; else eof=true; fi
	printf '{"text":%s,"start_cursor_bytes":0,"next_cursor_bytes":%s,"captured_bytes":%s,"eof":%s}' \
		"$(json_quote "$output")" "$bytes" "$bytes" "$eof"
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

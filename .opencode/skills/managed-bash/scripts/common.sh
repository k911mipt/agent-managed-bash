#!/bin/sh

die_usage() {
	printf '%s\n' "$*" >&2
	exit 2
}

require_nonnegative_bounded() {
	case $2 in ''|*[!0-9]*) die_usage "$1 must be a non-negative integer" ;; esac
	[ "$2" -le "$3" ] || die_usage "$1 must be at most $3"
}

require_positive_bounded() {
	require_nonnegative_bounded "$1" "$2" "$3"
	[ "$2" -gt 0 ] || die_usage "$1 must be greater than zero"
}

require_job_id() {
	case $1 in ''|*[!A-Za-z0-9_-]*) die_usage 'job ID has an invalid format' ;; esac
	[ "${#1}" -le 64 ] || die_usage 'job ID must be at most 64 characters'
}

require_session_id() {
	case $1 in ''|*[!A-Za-z0-9._:-]*) die_usage 'MANAGED_BASH_SESSION_ID has an invalid format' ;; esac
	[ "${#1}" -le 128 ] || die_usage 'MANAGED_BASH_SESSION_ID must be at most 128 characters'
}

require_command() {
	[ -n "$1" ] || die_usage 'command must not be empty'
	reject_unsupported_control_bytes "$1"
	bytes=$(printf '%s' "$1" | wc -c | tr -d ' ')
	[ "$bytes" -le 65536 ] || die_usage 'command must be at most 65536 bytes'
}

reject_unsupported_control_bytes() {
	JSON_VALUE=$1 LC_ALL=C awk 'BEGIN {
		value = ENVIRON["JSON_VALUE"]
		if (value ~ /[\001-\010\013\014\016-\037]/) exit 1
	}' || die_usage 'text contains an unsupported control byte'
}

json_quote() {
	reject_unsupported_control_bytes "$1"
	JSON_VALUE=$1 LC_ALL=C awk 'BEGIN {
		value = ENVIRON["JSON_VALUE"]
		gsub(/\\/, "\\\\", value)
		gsub(/"/, "\\\"", value)
		gsub(/\t/, "\\t", value)
		gsub(/\r/, "\\r", value)
		gsub(/\n/, "\\n", value)
		printf "\"%s\"", value
	}'
}

now_ms() {
	seconds=$(date +%s)
	printf '%s\n' "$((seconds * 1000))"
}

portable_os() {
	case $(uname -s) in
		Linux) printf '%s\n' linux ;;
		Darwin) printf '%s\n' darwin ;;
		*) printf '%s\n' unknown ;;
	esac
}

portable_arch() {
	case $(uname -m) in
		x86_64|amd64) printf '%s\n' amd64 ;;
		aarch64|arm64) printf '%s\n' arm64 ;;
		*) printf '%s\n' unknown ;;
	esac
}
